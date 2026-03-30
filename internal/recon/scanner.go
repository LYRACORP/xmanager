package recon

import (
	"fmt"
	"strings"
	"sync"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type RawResult struct {
	Category string
	Command  string
	Output   string
	Error    string
}

type ScanResult struct {
	Results []RawResult
	Raw     string
}

func Scan(exec *ssh.Executor) (*ScanResult, error) {
	scripts := AllScripts()

	var (
		mu      sync.Mutex
		results []RawResult
		wg      sync.WaitGroup
	)

	for _, cat := range scripts {
		for _, cmd := range cat.Commands {
			wg.Add(1)
			go func(category, command string) {
				defer wg.Done()

				res, err := exec.Run(command)

				r := RawResult{
					Category: category,
					Command:  command,
				}
				if err != nil {
					r.Error = err.Error()
				} else {
					r.Output = res.Stdout
					if res.Stderr != "" {
						r.Error = res.Stderr
					}
				}

				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}(cat.Name, cmd)
		}
	}

	wg.Wait()

	raw := formatRaw(results)

	return &ScanResult{
		Results: results,
		Raw:     raw,
	}, nil
}

func formatRaw(results []RawResult) string {
	grouped := make(map[string][]RawResult)
	for _, r := range results {
		grouped[r.Category] = append(grouped[r.Category], r)
	}

	var sb strings.Builder
	for _, cat := range AllScripts() {
		sb.WriteString(fmt.Sprintf("=== %s ===\n", strings.ToUpper(cat.Name)))
		for _, r := range grouped[cat.Name] {
			sb.WriteString(fmt.Sprintf("$ %s\n", r.Command))
			if r.Output != "" {
				sb.WriteString(r.Output + "\n")
			}
			if r.Error != "" {
				sb.WriteString("STDERR: " + r.Error + "\n")
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
