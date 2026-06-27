package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const tmuxSession = "omni"

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// up launches one Claude agent per brief (.md) in room .omni/<room>/, each in
// its own tmux window in the detached "omni" session, wired to omni's status
// hooks. The agents persist independently of any dashboard.
func up(room string) error {
	roomDir := filepath.Join(".omni", room)
	entries, err := os.ReadDir(roomDir)
	if err != nil {
		return fmt.Errorf("read room %q: %w", roomDir, err)
	}
	var briefs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			briefs = append(briefs, e.Name())
		}
	}
	sort.Strings(briefs)
	if len(briefs) == 0 {
		return fmt.Errorf("no .md briefs in %s", roomDir)
	}

	projectPath, _ := filepath.Abs(".")
	dbp := dbPath()
	db, err := openDB(dbp)
	if err != nil {
		return err
	}
	defer db.Close()

	self, err := os.Executable()
	if err != nil {
		return err
	}
	settingsPath, err := writeHookSettings(self, filepath.Dir(dbp))
	if err != nil {
		return err
	}
	if err := ensureSession(projectPath); err != nil {
		return err
	}

	for _, brief := range briefs {
		role := strings.TrimSuffix(brief, ".md")
		id := newID()
		now := time.Now().Unix()
		if _, err := db.Exec(`INSERT INTO sessions
			(id,room,project_path,role,tmux_pane,model,status,current_activity,started_at,last_event_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`,
			id, room, projectPath, role, "", "default", "starting", "", now, now); err != nil {
			return err
		}
		briefPath := filepath.Join(roomDir, brief)
		pane, err := launchWindow(role, projectPath, dbp, id, settingsPath, briefPath)
		if err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE sessions SET tmux_pane=? WHERE id=?`, pane, id); err != nil {
			return err
		}
		fmt.Printf("up: %-12s room=%s id=%s pane=%s\n", role, room, id, pane)
	}
	fmt.Printf("\n%d agent(s) up in tmux session %q.  View: omni   Attach: tmux attach -t %s\n",
		len(briefs), tmuxSession, tmuxSession)
	return nil
}

// writeHookSettings emits omni's status hooks as a settings file passed to
// claude via --settings, so the user's global/project config is never touched.
func writeHookSettings(omniBin, dir string) (string, error) {
	type cmd struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type entry struct {
		Hooks []cmd `json:"hooks"`
	}
	mk := func(ev string) []entry {
		return []entry{{Hooks: []cmd{{Type: "command", Command: fmt.Sprintf("%s hook %s", omniBin, ev)}}}}
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": mk("sessionstart"),
			"PreToolUse":   mk("pre"),
			"Notification": mk("notify"),
			"Stop":         mk("stop"),
			"SessionEnd":   mk("end"),
		},
	}
	b, _ := json.MarshalIndent(settings, "", "  ")
	p := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// ensureSession creates the detached "omni" tmux session if it isn't running.
func ensureSession(startDir string) error {
	if exec.Command("tmux", "has-session", "-t", tmuxSession).Run() == nil {
		return nil
	}
	return exec.Command("tmux", "new-session", "-d", "-s", tmuxSession, "-c", startDir).Run()
}

// launchWindow opens a new tmux window running a plain (user-authed) claude
// wired to omni's hooks, and returns its pane id. The agent's prompt just tells
// it to read its own brief — avoids passing multi-line markdown through tmux.
func launchWindow(role, dir, dbp, id, settings, briefPath string) (string, error) {
	prompt := fmt.Sprintf("Read your brief at %s, then begin working on it.", briefPath)
	claudeCmd := fmt.Sprintf("claude --settings %s %s", shellQuote(settings), shellQuote(prompt))
	out, err := exec.Command("tmux", "new-window", "-d", "-P", "-F", "#{pane_id}",
		"-t", tmuxSession, "-n", role, "-c", dir,
		"-e", "OMNI_DB="+dbp, "-e", "OMNI_ID="+id,
		claudeCmd).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
