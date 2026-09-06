// Package instrument ships ompool's SuperCollider instrument inside the binary
// and runs it. The audio view starts sclang on the embedded ompool.scd so a
// single `go install` gives people the sound as well as the dashboard; the
// only thing they need installed is SuperCollider itself.
package instrument

import (
	_ "embed"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Source is the instrument as a SuperCollider script. `ompool instrument`
// prints it so people can open it in SCIDE and change the tunables.
//
//go:embed ompool.scd
var Source string

// TestSender is a script that fakes the live feed so the instrument can be
// auditioned without waiting on the network.
//
//go:embed test_sender.scd
var TestSender string

// Env is the environment variable that controls the launcher: unset or
// "auto" starts the embedded instrument, "off" never starts anything (use
// this when running the instrument yourself in SCIDE), and a path runs that
// .scd instead of the embedded one.
const Env = "OMPOOL_INSTRUMENT"

// State describes where the launcher got to, for display in the audio view.
type State int

const (
	// Off means the launcher was disabled with OMPOOL_INSTRUMENT=off.
	Off State = iota
	// External means something already listens on the OSC port, so ompool
	// left it alone and is feeding that instead (typically SCIDE).
	External
	// Missing means sclang is not on PATH.
	Missing
	// Starting means sclang is compiling its class library and booting the
	// server, which takes a few seconds.
	Starting
	// Running means the script reported itself ready.
	Running
	// Exited means sclang stopped on its own; the log has the reason.
	Exited
	// Failed means sclang could not be started at all.
	Failed
)

// Process is a running (or finished) sclang.
type Process struct {
	cmd     *exec.Cmd
	logPath string

	mu    sync.Mutex
	state State
	err   error
}

// Status reports the process state and a short line suitable for a panel.
func (p *Process) Status() (State, string) {
	if p == nil {
		return Off, "instrument off"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch p.state {
	case Off:
		return Off, "instrument off (" + Env + "=off)"
	case External:
		return External, "OSC port already held (SCIDE?): feeding that instrument instead"
	case Missing:
		return Missing, "instrument needs SuperCollider: install it so sclang is on PATH"
	case Starting:
		return Starting, "instrument starting (sclang booting)…"
	case Running:
		return Running, "instrument running (sclang)"
	case Exited:
		return Exited, "instrument exited, see " + p.logPath
	default:
		msg := "instrument failed to start"
		if p.err != nil {
			msg += ": " + p.err.Error()
		}
		return Failed, msg
	}
}

func (p *Process) set(state State, err error) {
	p.mu.Lock()
	p.state = state
	p.err = err
	p.mu.Unlock()
}

// Start decides whether to launch sclang for the given OSC destination and
// does so. It never returns nil, so callers can always ask for Status. The
// process is detached from the terminal: its output goes to a log file under
// the user cache directory, and stopping it also stops the scsynth it booted.
func Start(oscAddr string) *Process {
	p := &Process{}
	mode := strings.TrimSpace(os.Getenv(Env))
	if strings.EqualFold(mode, "off") {
		p.state = Off
		return p
	}
	if portInUse(oscAddr) {
		p.state = External
		return p
	}
	sclang, err := exec.LookPath("sclang")
	if err != nil {
		p.state = Missing
		return p
	}

	dir, err := cacheDir()
	if err != nil {
		p.state, p.err = Failed, err
		return p
	}
	script, err := scriptPath(mode, dir)
	if err != nil {
		p.state, p.err = Failed, err
		return p
	}
	p.logPath = filepath.Join(dir, "instrument.log")
	logFile, err := os.Create(p.logPath)
	if err != nil {
		p.state, p.err = Failed, err
		return p
	}

	args := []string{}
	if port := oscPort(oscAddr); port != "" {
		args = append(args, "-u", port)
	}
	args = append(args, script)
	cmd := exec.Command(sclang, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Dir = filepath.Dir(script)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		logFile.Close()
		p.state, p.err = Failed, err
		return p
	}
	p.cmd = cmd
	p.state = Starting

	go func() {
		err := cmd.Wait()
		logFile.Close()
		p.mu.Lock()
		if p.state == Starting || p.state == Running {
			p.state, p.err = Exited, err
		}
		p.mu.Unlock()
	}()
	go p.watchReady()
	return p
}

// watchReady flips Starting to Running when the script prints its ready line.
// It polls the log rather than piping stdout so sclang never blocks on us.
func (p *Process) watchReady() {
	for range 120 {
		time.Sleep(250 * time.Millisecond)
		p.mu.Lock()
		state := p.state
		p.mu.Unlock()
		if state != Starting {
			return
		}
		data, err := os.ReadFile(p.logPath)
		if err == nil && strings.Contains(string(data), "instrument ready") {
			p.set(Running, nil)
			return
		}
	}
}

// Stop ends sclang and the server it booted. Safe on nil and on a process
// that was never started.
func (p *Process) Stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	p.mu.Lock()
	p.state = Off
	p.mu.Unlock()
	terminate(p.cmd)
	done := make(chan struct{})
	go func() {
		_, _ = p.cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = p.cmd.Process.Kill()
	}
}

// cacheDir is where the launcher keeps the script it runs and its log.
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "ompool")
	return dir, os.MkdirAll(dir, 0o755)
}

// scriptPath returns the .scd to run: the user's own if the env var names a
// file, otherwise the embedded instrument written to dir.
func scriptPath(mode, dir string) (string, error) {
	if mode != "" && !strings.EqualFold(mode, "auto") {
		if info, err := os.Stat(mode); err == nil && !info.IsDir() {
			return filepath.Abs(mode)
		}
		return "", fmt.Errorf("%s=%q is not a file", Env, mode)
	}
	path := filepath.Join(dir, "ompool.scd")
	if err := os.WriteFile(path, []byte(Source), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// portInUse reports whether a UDP listener already holds addr's port. sclang
// falls back to another port when its default is taken, which would leave a
// second instrument listening where ompool never sends; better to assume the
// existing listener is the instrument and feed it.
func portInUse(addr string) bool {
	port := oscPort(addr)
	if port == "" {
		return false
	}
	conn, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return true
	}
	conn.Close()
	return false
}

// oscPort extracts the port from host:port, or "" if it has none.
func oscPort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return ""
	}
	return port
}
