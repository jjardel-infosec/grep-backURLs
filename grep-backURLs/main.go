package main

import (
        "bufio"
        "fmt"
        "os"
        "os/exec"
        "regexp"
        "strings"
        "sync"
)

const (
        colorCyan  = "\033[36m"
        colorReset = "\033[0m"
)

type Pattern struct {
        original string
        regex    *regexp.Regexp
}

func parsePattern(keyword string) (*Pattern, error) {
        fmt.Printf("Debug: Processing pattern: %s\n", keyword)

        pattern := strings.TrimPrefix(keyword, "grep ")
        pattern = strings.TrimPrefix(pattern, "egrep ")
        pattern = strings.Trim(pattern, "\"'")

        var regexPattern string

        if strings.HasPrefix(pattern, "-") {
                parts := strings.Fields(pattern)
                if len(parts) > 1 {
                        pattern = parts[len(parts)-1]
                        fmt.Printf("Debug: After removing flags: %s\n", pattern)
                }
        }

        switch {
        case strings.HasPrefix(pattern, ".*"):
                regexPattern = pattern
                fmt.Printf("Debug: Wildcard pattern detected: %s\n", regexPattern)

        case strings.HasPrefix(pattern, "\\."):
                regexPattern = strings.Replace(pattern, "\\.", "\\.", -1)
                fmt.Printf("Debug: File extension pattern detected: %s\n", regexPattern)

        case strings.HasPrefix(pattern, "/"):
                regexPattern = regexp.QuoteMeta(pattern)
                fmt.Printf("Debug: Path pattern detected: %s\n", regexPattern)

        case strings.Contains(pattern, "(?<=") || strings.Contains(pattern, "(?="):
                regexPattern = pattern
                fmt.Printf("Debug: Lookaround pattern detected: %s\n", regexPattern)

        case strings.Contains(pattern, "(http|https)"):
                regexPattern = pattern
                fmt.Printf("Debug: URL pattern detected: %s\n", regexPattern)

        case strings.Contains(pattern, "[?&]"):
                regexPattern = pattern
                fmt.Printf("Debug: URL parameter pattern detected: %s\n", regexPattern)

        default:
                if !strings.Contains(pattern, "([") && !strings.Contains(pattern, "{") {
                        regexPattern = regexp.QuoteMeta(pattern)
                } else {
                        regexPattern = pattern
                }
                fmt.Printf("Debug: Default pattern handling: %s\n", regexPattern)
        }

        if strings.HasPrefix(keyword, "grep -w ") {
                regexPattern = "\\b" + regexPattern + "\\b"
                fmt.Printf("Debug: Added word boundaries: %s\n", regexPattern)
        }

        var flags string
        if strings.Contains(keyword, " -i ") || strings.HasPrefix(keyword, "grep -i ") {
                flags = "(?i)"
                fmt.Printf("Debug: Added case-insensitive flag\n")
        }

        regex, err := regexp.Compile(flags + regexPattern)
        if err != nil {
                return nil, fmt.Errorf("invalid pattern %s: %v", pattern, err)
        }

        fmt.Printf("Debug: Final regex pattern: %s\n", flags+regexPattern)

        return &Pattern{
                original: keyword,
                regex:    regex,
        }, nil
}

func isEmptyFile(filename string) bool {
        fileInfo, err := os.Stat(filename)
        if err != nil {
                fmt.Printf("Error checking file %s: %v\n", filename, err)
                return true
        }
        return fileInfo.Size() == 0
}

func runCommand(cmd *exec.Cmd, description string) error {
        fmt.Printf("Running %s...\n", description)
        if err := cmd.Run(); err != nil {
                fmt.Printf("Error running %s: %v\n", description, err)
                return err
        }
        fmt.Printf("%s completed successfully.\n", description)
        return nil
}

func processKeywords(waybackContent []byte, keywords []string, domain string) error {
        for _, keyword := range keywords {
                keyword = strings.TrimSpace(keyword)
                if keyword == "" {
                        continue
                }

                pattern, err := parsePattern(keyword)
                if err != nil {
                        fmt.Printf("Warning: Skipping invalid pattern '%s': %v\n", keyword, err)
                        continue
                }

                fmt.Printf("\n%sProcessing pattern: %s%s\n", colorCyan, pattern.original, colorReset)

                safeName := strings.Map(func(r rune) rune {
                        if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
                                return r
                        }
                        return '_'
                }, pattern.original)
                resultFile := fmt.Sprintf("%s_%s_results.txt", domain, safeName)

                var results []string
                scanner := bufio.NewScanner(strings.NewReader(string(waybackContent)))
                for scanner.Scan() {
                        line := scanner.Text()
                        if pattern.regex.MatchString(line) {
                                results = append(results, line)
                                fmt.Println(line)
                        }
                }

                if len(results) > 0 {
                        if err := os.WriteFile(resultFile, []byte(strings.Join(results, "\n")), 0644); err != nil {
                                return fmt.Errorf("error saving results for pattern %s: %v", pattern.original, err)
                        }
                }
        }
        return nil
}

func main() {
        var domain string
        fmt.Print("Enter the domain or URL (e.g., example.com): ")
        fmt.Scanln(&domain)

        if domain == "" {
                fmt.Println("Domain cannot be empty. Exiting.")
                return
        }

        if err := os.Mkdir(domain, 0755); err != nil {
                if !os.IsExist(err) {
                        fmt.Printf("Error creating directory %s: %v\n", domain, err)
                        return
                }
        }

        if err := os.Chdir(domain); err != nil {
                fmt.Printf("Error changing directory to %s: %v\n", domain, err)
                return
        }

        subFile := fmt.Sprintf("%s_subs.txt", domain)
        waybackFile := fmt.Sprintf("%s_wayback.txt", domain)
        grepKeywordsFile := "../grep_keywords.txt"

        if _, err := os.Stat(grepKeywordsFile); os.IsNotExist(err) {
                fmt.Printf("Error: %s file not found\n", grepKeywordsFile)
                return
        }

        var wg sync.WaitGroup

        wg.Add(1)
        go func() {
                defer wg.Done()
                cmd := exec.Command("subfinder", "-d", domain, "-o", subFile)
                if err := runCommand(cmd, "Subfinder"); err != nil {
                        return
                }
        }()

        wg.Wait()

        if isEmptyFile(subFile) {
                fmt.Println("No subdomains found. Exiting.")
                return
        }

        wg.Add(1)
        go func() {
                defer wg.Done()
                cmd := exec.Command("waybackurls")

                file, err := os.Open(subFile)
                if err != nil {
                        fmt.Printf("Error opening subdomain file: %v\n", err)
                        return
                }
                defer file.Close()

                cmd.Stdin = file
                outFile, err := os.Create(waybackFile)
                if err != nil {
                        fmt.Printf("Error creating wayback file: %v\n", err)
                        return
                }
                defer outFile.Close()

                cmd.Stdout = outFile
                if err := runCommand(cmd, "Waybackurls"); err != nil {
                        return
                }
        }()

        wg.Wait()

        if isEmptyFile(waybackFile) {
                fmt.Println("No Wayback URLs found. Exiting.")
                return
        }

        waybackContent, err := os.ReadFile(waybackFile)
        if err != nil {
                fmt.Printf("Error reading wayback file: %v\n", err)
                return
        }

		keywords, err:= os.ReadFile(grepKeywordsFile)
				if err!= nil {
						fmt.Printf("Error reading grep keywords file: %v\n", err)
						return
				}

				if err:= processKeywords(waybackContent, strings.Split(string(keywords), "\n"), domain); err!= nil {
						fmt.Printf("Error processing keywords: %v\n", err)
						return
				}

				fmt.Println("\nAutomation script completed successfully.")
		}
