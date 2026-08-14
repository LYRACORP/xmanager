package web

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

// containerWorkDir returns the container WORKDIR, defaulting to /app.
func containerWorkDir(container string) string {
	out, err := exec.Command("docker", "inspect", "-f", "{{.Config.WorkingDir}}", container).Output()
	if err != nil {
		return "/app"
	}
	wd := strings.TrimSpace(string(out))
	if wd == "" || wd == "/" {
		return "/app"
	}
	return path.Clean(wd)
}

func shellQuotePOSIX(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}

// resolveContainerPath joins workDir + rel and jails under workDir (POSIX paths).
func resolveContainerPath(workDir, rel string) (string, error) {
	workDir = path.Clean("/" + strings.TrimPrefix(workDir, "/"))
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return workDir, nil
	}
	if strings.HasPrefix(rel, "/") || rel == ".." || strings.HasPrefix(rel, "../") ||
		strings.Contains(rel, "/../") || strings.HasSuffix(rel, "/..") {
		return "", fmt.Errorf("invalid path")
	}
	abs := path.Clean(path.Join(workDir, rel))
	if abs != workDir && !strings.HasPrefix(abs, workDir+"/") {
		return "", fmt.Errorf("path escapes container workdir")
	}
	return abs, nil
}

func dockerExec(container string, args ...string) (string, error) {
	cmdArgs := append([]string{"exec", container}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("%s", msg)
	}
	return out, nil
}

func dockerExecBytes(container string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"exec", container}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return stdout.Bytes(), nil
}

func listContainerDir(container, workDir, rel string) ([]projectFileEntry, error) {
	abs, err := resolveContainerPath(workDir, rel)
	if err != nil {
		return nil, err
	}
	script := fmt.Sprintf(`
set +e
DIR=%s
if [ ! -d "$DIR" ]; then echo "NOTDIR"; exit 2; fi
ls -1A "$DIR" 2>/dev/null | while IFS= read -r name; do
  [ -z "$name" ] && continue
  p="$DIR/$name"
  if [ -d "$p" ]; then kind=d; else kind=f; fi
  size=0
  if [ -f "$p" ]; then size=$(wc -c < "$p" 2>/dev/null | tr -d ' \n'); fi
  mode=$(ls -ld "$p" 2>/dev/null | awk '{print $1}')
  mtime=$(date -r "$p" '+%%Y-%%m-%%d %%H:%%M' 2>/dev/null || true)
  if [ -z "$mtime" ]; then
    mtime=$(stat -c '%%Y' "$p" 2>/dev/null)
    if [ -n "$mtime" ]; then
      mtime=$(date -d "@$mtime" '+%%Y-%%m-%%d %%H:%%M' 2>/dev/null || echo '')
    fi
  fi
  printf '%%s\t%%s\t%%s\t%%s\t%%s\n' "$kind" "$name" "$size" "$mode" "$mtime"
done
`, shellQuotePOSIX(abs))
	out, err := dockerExec(container, "sh", "-c", script)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(out), "NOTDIR") {
		return nil, fmt.Errorf("not a directory")
	}
	var list []projectFileEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "NOTDIR" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			continue
		}
		kind, name, sizeStr, mode := parts[0], parts[1], parts[2], parts[3]
		mtime := ""
		if len(parts) > 4 {
			mtime = parts[4]
		}
		if name == "" || name == "." || name == ".." {
			continue
		}
		sz, _ := strconv.ParseInt(strings.TrimSpace(sizeStr), 10, 64)
		childRel := name
		if strings.Trim(rel, "/") != "" {
			childRel = strings.Trim(rel, "/") + "/" + name
		}
		if mtime == "" {
			mtime = time.Now().Format("2006-01-02 15:04")
		}
		list = append(list, projectFileEntry{
			Name:      name,
			RelPath:   childRel,
			Mode:      mode,
			IsDir:     kind == "d",
			Size:      sz,
			SizeHuman: formatFileSize(sz),
			Mod:       mtime,
		})
	}
	return list, nil
}

func readContainerFile(container, workDir, rel string) ([]byte, error) {
	abs, err := resolveContainerPath(workDir, rel)
	if err != nil {
		return nil, err
	}
	szOut, err := dockerExec(container, "sh", "-c", "wc -c < "+shellQuotePOSIX(abs))
	if err != nil {
		return nil, err
	}
	sz, _ := strconv.ParseInt(strings.TrimSpace(szOut), 10, 64)
	if sz > maxFileEditBytes {
		return nil, fmt.Errorf("file larger than 10 MiB — download instead")
	}
	return dockerExecBytes(container, "cat", abs)
}

func writeContainerFile(container, workDir, rel string, data []byte) error {
	abs, err := resolveContainerPath(workDir, rel)
	if err != nil {
		return err
	}
	cmd := exec.Command("docker", "exec", "-i", container, "sh", "-c", "cat > "+shellQuotePOSIX(abs))
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func mkdirContainer(container, workDir, rel string) error {
	abs, err := resolveContainerPath(workDir, rel)
	if err != nil {
		return err
	}
	_, err = dockerExec(container, "mkdir", "-p", abs)
	return err
}

func createContainerFile(container, workDir, rel string) error {
	abs, err := resolveContainerPath(workDir, rel)
	if err != nil {
		return err
	}
	_, err = dockerExec(container, "sh", "-c", "set -e; if [ -e "+shellQuotePOSIX(abs)+" ]; then exit 1; fi; : > "+shellQuotePOSIX(abs))
	return err
}

func renameContainerPath(container, workDir, oldRel, newRel string) error {
	oldAbs, err := resolveContainerPath(workDir, oldRel)
	if err != nil {
		return err
	}
	newAbs, err := resolveContainerPath(workDir, newRel)
	if err != nil {
		return err
	}
	_, err = dockerExec(container, "mv", oldAbs, newAbs)
	return err
}

func deleteContainerPath(container, workDir, rel string) error {
	abs, err := resolveContainerPath(workDir, rel)
	if err != nil {
		return err
	}
	if abs == path.Clean(workDir) {
		return fmt.Errorf("cannot delete container workdir")
	}
	_, err = dockerExec(container, "rm", "-rf", abs)
	return err
}

func chmodContainerPath(container, workDir, rel, modeOctal string) error {
	abs, err := resolveContainerPath(workDir, rel)
	if err != nil {
		return err
	}
	_, err = dockerExec(container, "chmod", modeOctal, abs)
	return err
}

func uploadContainerFile(container, workDir, relDir, filename string, r io.Reader) error {
	child := filename
	if relDir != "" {
		child = strings.Trim(relDir, "/") + "/" + filename
	}
	abs, err := resolveContainerPath(workDir, child)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "xm-upload-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	n, err := io.Copy(tmp, io.LimitReader(r, maxFileUploadBytes+1))
	_ = tmp.Close()
	if err != nil {
		return err
	}
	if n > maxFileUploadBytes {
		return fmt.Errorf("upload exceeds 100 MiB limit")
	}
	_, _ = dockerExec(container, "mkdir", "-p", path.Dir(abs))
	cmd := exec.Command("docker", "cp", tmpPath, container+":"+abs)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
