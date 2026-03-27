package tui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ZeroSegFault/linx/agent"
	"github.com/ZeroSegFault/linx/agent/providers"
	"github.com/ZeroSegFault/linx/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// agentResponseMsg carries a completed agent response.
type agentResponseMsg struct {
	response string
	err      error
}

// agentEventMsg carries an intermediate agent event for live display.
type agentEventMsg struct {
	event agent.Event
}

// confirmRequestMsg asks the user to confirm an action.
type confirmRequestMsg struct {
	description string
	responseCh  chan bool
}

type model struct {
	cfg      *config.Config
	program  *tea.Program // set after program creation
	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model
	keys     keyMap
	ready    bool
	width    int
	height   int

	// Output buffer
	output *strings.Builder

	// State
	thinking       bool
	statusMsg      string
	confirmPending bool
	confirmDesc    string
	confirmCh      chan bool

	// Persistent session
	agent   *agent.Agent   // persists across prompts
	session *agent.Session // session file tracker

	// Crash recovery
	recovering      bool             // true when showing recovery prompt
	crashedSessions []*agent.Session // crashed sessions found on startup

	// Async memory extraction
	extractWg *sync.WaitGroup // tracks in-flight memory extractions

	// Session restore
	restoredMessages []providers.Message // messages from a restored session
}

// cleanupSession archives the session and extracts memory on clean exit.
func cleanupSession(m *model) {
	// Wait for any in-flight memory extraction to finish
	m.extractWg.Wait()

	if m.session != nil {
		m.session.Archive()
	}
	if m.agent != nil {
		m.agent.ExtractAndSaveMemory() // one final extraction
	}
}

// Run starts the TUI application.
func Run(cfg *config.Config) error {
	// Detect crashed sessions before starting TUI
	crashedSessions, _ := agent.DetectCrashed()

	// Create session for this run
	profile := cfg.DefaultProfile
	if profile == "" {
		profile = "(default)"
	}
	sess, sessErr := agent.NewSession(cfg.Provider.Model, profile)

	m := initialModel(cfg)
	m.session = sess
	if sessErr != nil {
		m.output.WriteString(fmt.Sprintf("⚠ Could not create session: %v\n", sessErr))
	}

	// If crashed sessions found, show recovery prompt
	if len(crashedSessions) > 0 {
		m.recovering = true
		m.crashedSessions = crashedSessions
		m.output.Reset()
		m.output.WriteString("🔄 Previous session found:\n\n")
		for i, s := range crashedSessions {
			summary := s.Summary()
			m.output.WriteString(fmt.Sprintf("  %d. %s\n", i+1, summary))
		}
		m.output.WriteString("\nPress [r] to restore the last session, or [n] for a new session.\n")
	}

	p := tea.NewProgram(&m, tea.WithAltScreen())
	m.program = p

	_, err := p.Run()

	cleanupSession(&m)
	return err
}

// RunWithSession starts the TUI with an existing session (for --resume).
func RunWithSession(cfg *config.Config, sess *agent.Session) error {
	m := initialModel(cfg)
	m.session = sess
	m.restoredMessages = sess.RebuildMessages("")

	// Show resumed session info
	m.output.Reset()
	m.output.WriteString(fmt.Sprintf("📂 Resumed session %s (%d turns)\n\n", sess.UUID[:8], len(sess.Turns)))

	p := tea.NewProgram(&m, tea.WithAltScreen())
	m.program = p
	_, err := p.Run()

	cleanupSession(&m)
	return err
}

func initialModel(cfg *config.Config) model {
	ta := textarea.New()
	ta.Placeholder = "Ask Linx anything about your system..."
	ta.Focus()
	ta.CharLimit = 2000
	ta.SetHeight(3)
	ta.ShowLineNumbers = false

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED"))

	m := model{
		cfg:       cfg,
		textarea:  ta,
		spinner:   s,
		keys:      defaultKeyMap(),
		output:    &strings.Builder{},
		extractWg: &sync.WaitGroup{},
	}

	m.output.WriteString("Welcome to Linx 🐧\nType a question or command to get started.\n\n")

	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 1  // title
		statusHeight := 1  // status bar
		inputHeight := 5   // textarea + border
		helpHeight := 1    // help line
		chrome := headerHeight + statusHeight + inputHeight + helpHeight + 2

		vpHeight := m.height - chrome
		if vpHeight < 3 {
			vpHeight = 3
		}

		if !m.ready {
			m.viewport = viewport.New(m.width, vpHeight)
			m.viewport.SetContent(m.output.String())
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = vpHeight
		}

		m.textarea.SetWidth(m.width - 4)

	case tea.KeyMsg:
		// Recovery prompt handling
		if m.recovering {
			switch msg.String() {
			case "r", "R":
				m.recovering = false
				if len(m.crashedSessions) > 0 {
					crashed := m.crashedSessions[0] // restore most recent
					if err := agent.RestoreFromCrashed(crashed); err != nil {
						m.output.Reset()
						m.output.WriteString(fmt.Sprintf("⚠ Could not restore session: %v\n\n", err))
						m.output.WriteString("Starting new session.\n\n")
					} else {
						if m.session != nil {
							m.session.Archive()
						}
						m.session = crashed
						// Rebuild messages for agent restore
						m.restoredMessages = crashed.RebuildMessages("")
						m.output.Reset()
						m.output.WriteString("✅ Session restored! Continuing where you left off.\n\n")
					}
					for _, s := range m.crashedSessions[1:] {
						s.Archive()
					}
				}
				m.crashedSessions = nil
				m.viewport.SetContent(m.output.String())
				m.viewport.GotoBottom()
				return m, nil
			case "n", "N":
				m.recovering = false
				for _, s := range m.crashedSessions {
					s.Archive()
				}
				m.crashedSessions = nil
				m.output.Reset()
				m.output.WriteString("Welcome to Linx 🐧\nType a question or command to get started.\n\n")
				m.viewport.SetContent(m.output.String())
				m.viewport.GotoBottom()
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil // ignore other keys during recovery
		}

		if m.confirmPending {
			switch msg.String() {
			case "y", "Y":
				m.confirmPending = false
				if m.confirmCh != nil {
					m.confirmCh <- true
					m.confirmCh = nil
				}
				m.appendOutput(toolDoneStyle.Render("✓ Confirmed"))
				return m, nil
			case "n", "N":
				m.confirmPending = false
				if m.confirmCh != nil {
					m.confirmCh <- false
					m.confirmCh = nil
				}
				m.appendOutput(errorStyle.Render("✗ Denied"))
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+l":
			m.output.Reset()
			m.output.WriteString("Screen cleared.\n\n")
			m.viewport.SetContent(m.output.String())
			m.viewport.GotoBottom()
			// Archive current session and start fresh
			if m.session != nil {
				m.session.Archive()
			}
			if m.agent != nil {
				convSnapshot := m.agent.ConversationSnapshot()
				m.extractWg.Add(1)
				go func() {
					defer m.extractWg.Done()
					m.agent.ExtractAndSaveMemoryFromSnapshot(convSnapshot)
				}()
			}
			m.agent = nil // will be recreated on next prompt
			// Create new session
			profile := m.cfg.DefaultProfile
			if profile == "" {
				profile = "(default)"
			}
			m.session, _ = agent.NewSession(m.cfg.Provider.Model, profile)
			m.output.WriteString("Session cleared. Starting fresh.\n\n")
			m.viewport.SetContent(m.output.String())
			return m, nil
		case "enter":
			if m.thinking || m.recovering {
				return m, nil
			}
			input := strings.TrimSpace(m.textarea.Value())
			if input == "" {
				return m, nil
			}
			m.textarea.Reset()
			m.appendOutput(fmt.Sprintf("\n> %s\n", input))
			m.thinking = true
			m.statusMsg = "Thinking..."
			return m, m.runAgent(input)
		}

	case agentEventMsg:
		switch msg.event.Type {
		case agent.EventThinking:
			m.statusMsg = msg.event.Message
		case agent.EventToolCall:
			m.appendOutput(toolCallStyle.Render(msg.event.Message))
		case agent.EventToolResult:
			m.appendOutput(toolDoneStyle.Render(msg.event.Message))
		case agent.EventError:
			m.appendOutput(errorStyle.Render("Error: " + msg.event.Message))
		case agent.EventStreamChunk:
			// Append chunk directly without newline — partial text
			m.output.WriteString(msg.event.Data)
			m.viewport.SetContent(m.output.String())
			m.viewport.GotoBottom()
		}
		return m, nil

	case agentResponseMsg:
		m.thinking = false
		m.statusMsg = ""
		if msg.err != nil {
			m.appendOutput(errorStyle.Render(fmt.Sprintf("\nError: %v\n", msg.err)))
		} else {
			// Add a newline after streamed content
			m.appendOutput("")
		}
		return m, nil

	case confirmRequestMsg:
		m.confirmPending = true
		m.confirmDesc = msg.description
		m.confirmCh = msg.responseCh
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	if !m.confirmPending {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) appendOutput(s string) {
	m.output.WriteString(s + "\n")
	m.viewport.SetContent(m.output.String())
	m.viewport.GotoBottom()
}

func (m *model) runAgent(prompt string) tea.Cmd {
	cfg := m.cfg
	prog := m.program
	// Capture before the goroutine
	currentAgent := m.agent
	currentSession := m.session

	return func() tea.Msg {
		a := currentAgent
		if a == nil {
			confirmFn := func(desc string) bool {
				ch := make(chan bool, 1)
				if prog != nil {
					prog.Send(confirmRequestMsg{description: desc, responseCh: ch})
				} else {
					return false
				}
				return <-ch
			}
			callbackFn := func(event agent.Event) {
				if prog != nil {
					prog.Send(agentEventMsg{event: event})
				}
			}
			var err error
			a, err = agent.New(cfg, confirmFn, callbackFn)
			if err != nil {
				return agentResponseMsg{err: err}
			}
			// Store back — safe because thinking=true prevents concurrent runAgent
			m.agent = a

			// Load restored messages if available
			if len(m.restoredMessages) > 0 {
				systemPrompt := a.BuildSystemPrompt()
				if len(m.restoredMessages) > 0 && m.restoredMessages[0].Role == "system" {
					m.restoredMessages[0].Content = systemPrompt
				}
				a.LoadMessages(m.restoredMessages)
				m.restoredMessages = nil
			}
		}

		response, turn, err := a.ChatTurn(prompt)
		if err != nil {
			return agentResponseMsg{err: err}
		}

		if currentSession != nil {
			currentSession.AddTurn(turn)
		}

		// Extract memory in background — don't block the UI
		convSnapshot := a.ConversationSnapshot()
		m.extractWg.Add(1)
		go func() {
			defer m.extractWg.Done()
			a.ExtractAndSaveMemoryFromSnapshot(convSnapshot)
		}()

		return agentResponseMsg{response: response, err: nil}
	}
}
