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
// hooks AND the room's isolated hcom message bus. The agents persist
// independently of any dashboard.
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

	// Per-room isolated hcom bus: a co-located .hcom whose HCOM_DIR each agent
	// inherits via the tmux window env. Two rooms => two HCOM_DIRs => zero
	// cross-talk. Standalone (no-room) launches never get here, so structurally
	// they get status hooks but no bus (SPEC: "monitored-but-mute").
	hcomDir, err := filepath.Abs(filepath.Join(roomDir, ".hcom"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(hcomDir, 0o755); err != nil {
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
		pane, err := launchWindow(role, projectPath, dbp, id, settingsPath, briefPath, hcomDir)
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

// writeHookSettings emits a single --settings file carrying BOTH omni's status
// hooks and hcom's bus hooks, so the user's global/project config is never
// touched and agents launched with cwd=repo-root load everything from one file.
func writeHookSettings(omniBin, dir string) (string, error) {
	hcom, err := hcomSettings()
	if err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(buildSettings(omniBin, hcom), "", "  ")
	p := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// hcomSettings returns hcom's hook config (hooks/env/permissions) by running the
// real `hcom hooks add claude` into a throwaway temp dir and reading what it
// generates at <parent-of-HCOM_DIR>/.claude/settings.json.
//
// ponytail: generate-and-merge rather than hardcoding hcom's hook JSON. Embedding
// the literal entries would silently drift if hcom changes its hook format
// (verb names, the 86400 timeouts, the PreToolUse matcher, the allow-list).
// HCOM_DIR here is a throwaway only used to make hcom emit; the *real* per-room
// HCOM_DIR is supplied to agents via the tmux window env, so this merged file
// stays room-agnostic and is shared across rooms.
func hcomSettings() (map[string]any, error) {
	tmp, err := os.MkdirTemp("", "omni-hcom")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	hcomDir := filepath.Join(tmp, ".hcom")
	cmd := exec.Command("hcom", "hooks", "add", "claude")
	cmd.Env = append(os.Environ(), "HCOM_DIR="+hcomDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("hcom hooks add: %w: %s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(tmp, ".claude", "settings.json"))
	if err != nil {
		return nil, fmt.Errorf("read generated hcom settings: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse hcom settings: %w", err)
	}
	return m, nil
}

// buildSettings merges omni's status hooks with hcom's bus config into one
// settings map. For events both touch, BOTH hook entries are kept (concatenated)
// so status updates and bus delivery run on every event; hcom-only events and
// hcom's env (HCOM=hcom) and permissions.allow (pre-approves `hcom start`/`send`
// so agents run them without a prompt) are carried through verbatim. Pure and
// testable — no Claude/tmux spawn.
func buildSettings(omniBin string, hcom map[string]any) map[string]any {
	cmdEntry := func(ev string) any {
		return map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": fmt.Sprintf("%s hook %s", omniBin, ev),
			}},
		}
	}
	// omni's status hooks, keyed by Claude event => omni verb.
	omniHooks := map[string]any{
		"SessionStart": []any{cmdEntry("sessionstart")},
		"PreToolUse":   []any{cmdEntry("pre")},
		"Notification": []any{cmdEntry("notify")},
		"Stop":         []any{cmdEntry("stop")},
		"SessionEnd":   []any{cmdEntry("end")},
	}

	merged := map[string]any{}
	if h, ok := hcom["hooks"].(map[string]any); ok {
		for ev, v := range h {
			merged[ev] = v
		}
	}
	// Prepend omni's entries so status updates land before bus delivery.
	for ev, oe := range omniHooks {
		oList := oe.([]any)
		if existing, ok := merged[ev].([]any); ok {
			merged[ev] = append(append([]any{}, oList...), existing...)
		} else {
			merged[ev] = oList
		}
	}

	settings := map[string]any{"hooks": merged}
	if env, ok := hcom["env"]; ok {
		settings["env"] = env
	}
	if perms, ok := hcom["permissions"]; ok {
		settings["permissions"] = perms
	}
	return settings
}

// ensureSession creates the detached "omni" tmux session if it isn't running.
func ensureSession(startDir string) error {
	if exec.Command("tmux", "has-session", "-t", tmuxSession).Run() == nil {
		return nil
	}
	return exec.Command("tmux", "new-session", "-d", "-s", tmuxSession, "-c", startDir).Run()
}

// launchWindow opens a new tmux window running a plain (user-authed) claude
// wired to omni's hooks + the room's hcom bus, and returns its pane id.
//
// Addressing scheme (load-bearing for issues #3/#6): HCOM_TAG=<role> tags this
// agent on the bus. role is the brief filename without .md, unique within a room,
// so omni can later address exactly this agent as @<role> (per hcom: tagged
// agents are reachable as @<tag> and @<tag>-<name>). HCOM_DIR points at the
// room's isolated .hcom; its instances table records name/tag/directory.
//
// The prompt makes the agent self-join: plain claude + hcom hooks does NOT
// auto-join, so it must run `hcom start` once (pre-approved by hcom's
// permissions.allow Bash(hcom start:*)) to get an identity + inbox.
func launchWindow(role, dir, dbp, id, settings, briefPath, hcomDir string) (string, error) {
	prompt := fmt.Sprintf("First run: hcom start   (joins your team message bus). "+
		"Then read your brief at %s and begin working on it.", briefPath)
	claudeCmd := fmt.Sprintf("claude --settings %s %s", shellQuote(settings), shellQuote(prompt))
	out, err := exec.Command("tmux", "new-window", "-d", "-P", "-F", "#{pane_id}",
		"-t", tmuxSession, "-n", role, "-c", dir,
		"-e", "OMNI_DB="+dbp, "-e", "OMNI_ID="+id,
		"-e", "HCOM_DIR="+hcomDir, "-e", "HCOM_TAG="+role,
		claudeCmd).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
