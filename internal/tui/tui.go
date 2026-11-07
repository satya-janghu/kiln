// Package tui is the Bubble Tea v2 wizard (docs/tui-design.md, D42).
//
// Screens: 0 prechecks, 1 facts, 2 checklist, 3 options forms, 4 review,
// 5 run, 6 summary. The look follows the Windows XP text-mode installer:
// blue on every cell, light grey text, double-line panels, a yellow progress
// bar and a light grey status bar.
//
// Process model (docs/architecture.md, D41): the runner and every repair run
// in their own goroutines, owned by Run and not by the Bubble Tea program, so
// losing the terminal never stops them. Events reach the UI through an
// unbounded queue, so a stalled terminal can never block the runner.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"

	"github.com/satya-janghu/kiln/internal/app"
	"github.com/satya-janghu/kiln/internal/apt"
	"github.com/satya-janghu/kiln/internal/logx"
	"github.com/satya-janghu/kiln/internal/runner"
)

var itoa = strconv.Itoa

// Run starts the wizard. It returns only after any run or repair it started
// has finished, even if the terminal went away in the meantime.
func Run(ctx context.Context, a *app.App, force bool) error {
	// Closing the terminal must not stop a run (architecture: "Terminal loss").
	signal.Ignore(syscall.SIGHUP)

	release, pid, lockErr := a.LockRun()
	var once sync.Once
	unlock := func() {
		once.Do(func() {
			if release != nil {
				release()
			}
		})
	}
	defer unlock()

	c := newController(ctx, a)
	m := newModel(c, force, pid, lockErr)
	// kiln handles SIGINT/SIGTERM itself: stop after the current step, never
	// tear down the UI in the middle of a run.
	opts := []tea.ProgramOption{tea.WithoutSignalHandler()}
	var p *tea.Program
	var goneOnce sync.Once
	gone := func(err error) {
		// EIO/EPIPE/EOF on the terminal: the UI is gone. Bubble Tea ignores
		// write errors and treats EOF as "no more input", so end the program
		// ourselves; Run then waits for any run in progress.
		goneOnce.Do(func() {
			a.Log.Warn("the terminal went away", "err", err.Error())
			go p.Quit()
		})
	}
	if term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()) {
		opts = append(opts, tea.WithInput(&ttyFile{f: os.Stdin, gone: gone}), tea.WithOutput(&ttyFile{f: os.Stdout, gone: gone}))
	}
	p = tea.NewProgram(m, opts...)
	done := make(chan struct{})
	sigDone := make(chan struct{})
	go c.q.pump(p, done)
	// The signal watcher outlives the screen: after a lost terminal, SIGTERM
	// and SIGINT must still mean "stop after the current step" (D41).
	go c.watchSignals(p, sigDone)
	defer close(sigDone)

	_, err := p.Run()
	close(done)
	c.q.close()

	if c.busy() {
		// The screen is gone (or the program ended) while a run or repair is
		// in progress: keep going; everything is in /var/log/kiln.log.
		if err != nil {
			a.Log.Warn("the screen went away; kiln carries on and logs to "+logx.Path, "err", err.Error())
		}
		c.wg.Wait()
		return nil
	}
	c.wg.Wait()
	if c.rebootRequested() {
		unlock()
		a.Log.Info("rebooting at the user's request")
		return runner.Reboot(c.bg, a.Real)
	}
	if err != nil && (errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled)) {
		return nil
	}
	return err
}

// ttyFile wraps the terminal so kiln notices when it goes away. It is still
// a term.File (Fd), so Bubble Tea treats it as the terminal (raw mode, size).
type ttyFile struct {
	f    *os.File
	gone func(error)
}

func (t *ttyFile) Read(b []byte) (int, error) {
	n, err := t.f.Read(b)
	if err != nil && n == 0 {
		t.gone(err)
	}
	return n, err
}

func (t *ttyFile) Write(b []byte) (int, error) {
	n, err := t.f.Write(b)
	if err != nil {
		t.gone(err)
	}
	return n, err
}

// Close leaves the process's stdin and stdout open.
func (t *ttyFile) Close() error { return nil }

func (t *ttyFile) Fd() uintptr { return t.f.Fd() }

// controller owns everything that must outlive the Bubble Tea program.
type controller struct {
	ctx context.Context // cancelled on SIGTERM; used for read-only work
	bg  context.Context // never cancelled; runs and repairs finish on their own
	a   *app.App
	q   *queue
	wg  sync.WaitGroup

	tasks  atomic.Int32 // repairs in progress
	mu     sync.Mutex
	run    *runner.Runner
	active bool // a run is in progress
	reboot bool
}

func newController(ctx context.Context, a *app.App) *controller {
	return &controller{ctx: ctx, bg: context.WithoutCancel(ctx), a: a, q: newQueue()}
}

func (c *controller) busy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active || c.tasks.Load() > 0
}

func (c *controller) requestReboot() {
	c.mu.Lock()
	c.reboot = true
	c.mu.Unlock()
}

func (c *controller) rebootRequested() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reboot
}

// startRun runs the steps for real in a goroutine of its own.
func (c *controller) startRun() {
	r := c.a.Runner(func(e runner.Event) { c.q.push(eventMsg{e}) })
	c.mu.Lock()
	c.run, c.active = r, true
	c.mu.Unlock()
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		var sum *runner.Summary
		defer func() {
			msg := runDoneMsg{sum: sum}
			if p := recover(); p != nil {
				c.a.Log.Error("the run hit an internal error", "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
				msg.err = fmt.Errorf("kiln hit an internal error during the run (%v)", p)
			}
			c.mu.Lock()
			c.active = false
			c.mu.Unlock()
			c.q.push(msg)
		}()
		sum = r.Run(c.bg)
	}()
}

// stopRun asks the runner to stop after the current step. It reports
// whether a run was in progress.
func (c *controller) stopRun() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active && c.run != nil {
		c.run.Stop()
		return true
	}
	return false
}

// task runs a repair (revert, dpkg repair, stale masks) that must finish even
// if the UI goes away. Its result arrives as a taskDoneMsg.
func (c *controller) task(kind taskKind, fn func(ctx context.Context) error) {
	c.wg.Add(1)
	c.tasks.Add(1)
	go func() {
		var err error
		defer func() {
			if p := recover(); p != nil {
				c.a.Log.Error("internal error", "task", int(kind), "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
				err = fmt.Errorf("kiln hit an internal error (%v)", p)
			}
			c.tasks.Add(-1)
			c.wg.Done()
			c.q.push(taskDoneMsg{kind: kind, err: err})
		}()
		if kind == taskRepair {
			apt.Output = func(l string) { c.q.push(taskOutputMsg{line: l}) }
			defer func() { apt.Output = nil }()
		}
		err = fn(c.bg)
	}()
}

// watchSignals: SIGINT/SIGTERM stop a run after its current step and let a
// repair finish; otherwise they end the wizard.
func (c *controller) watchSignals(p *tea.Program, done <-chan struct{}) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	for {
		select {
		case <-done:
			return
		case s := <-sig:
			c.a.Log.Warn("got a stop signal", "signal", s.String())
			if c.stopRun() || c.tasks.Load() > 0 {
				c.q.push(signalMsg{})
				continue
			}
			p.Quit()
		}
	}
}

// --- event queue ------------------------------------------------------------

// queue decouples senders (runner, repairs) from the UI: push never blocks.
type queue struct {
	mu     sync.Mutex
	items  []tea.Msg
	wake   chan struct{}
	closed bool
}

const queueSoftCap = 20000

func newQueue() *queue { return &queue{wake: make(chan struct{}, 1)} }

func (q *queue) push(m tea.Msg) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	if len(q.items) >= queueSoftCap && droppable(m) {
		q.mu.Unlock()
		return // the full log still has it
	}
	q.items = append(q.items, m)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func droppable(m tea.Msg) bool {
	switch v := m.(type) {
	case eventMsg:
		return v.e.Kind == runner.EvOutput || v.e.Kind == runner.EvProgress
	case taskOutputMsg:
		return true
	}
	return false
}

func (q *queue) close() {
	q.mu.Lock()
	q.closed, q.items = true, nil
	q.mu.Unlock()
}

// pump forwards queued messages to the program in batches. p.Send returns at
// once after the program has ended.
func (q *queue) pump(p *tea.Program, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case <-q.wake:
		}
		q.mu.Lock()
		items := q.items
		q.items = nil
		q.mu.Unlock()
		if len(items) > 0 {
			p.Send(batchMsg(items))
		}
	}
}

// --- messages ---------------------------------------------------------------

type batchMsg []tea.Msg

type eventMsg struct{ e runner.Event }

type runDoneMsg struct {
	sum *runner.Summary
	err error
}

type taskKind int

const (
	taskRevert taskKind = iota
	taskRepair
	taskMasks
)

type taskDoneMsg struct {
	kind taskKind
	err  error
}

type taskOutputMsg struct{ line string }

type signalMsg struct{}
