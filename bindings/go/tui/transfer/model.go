package transfer

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"ocm.software/open-component-model/bindings/go/tui/fetch"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// Wizard steps
type step int

const (
	stepSource step = iota
	stepTarget
	stepOptions
	stepReview
	stepExecuting
	stepDone
)

// Messages
type graphBuiltMsg struct {
	tgd *transformv1alpha1.TransformationGraphDefinition
}

type graphErrMsg struct{ err error }

type transferProgressMsg struct {
	progress fetch.TransferProgress
}

type transferDoneMsg struct{ err error }

// Model is the bubbletea model for the step-by-step transfer wizard.
type Model struct {
	executor fetch.TransferExecutor
	keys     KeyMap

	step step

	// Inputs
	sourceInput textinput.Model
	targetInput textinput.Model

	// Options
	recursive     bool
	copyResources bool
	uploadAs      int // 0=default, 1=localBlob, 2=ociArtifact
	optionCursor  int

	// Review
	tgd        *transformv1alpha1.TransformationGraphDefinition
	reviewView viewport.Model

	// Execution
	progressCh      <-chan fetch.TransferProgress
	doneCh          <-chan error
	progressLog     []string
	progressCurrent int
	progressTotal   int
	execErr         error

	// Layout
	width  int
	height int
	ready  bool
	err    error
}

var uploadAsLabels = []string{"default", "localBlob", "ociArtifact"}

// New creates a new transfer wizard model.
func New(executor fetch.TransferExecutor) Model {
	src := textinput.New()
	src.Placeholder = "ghcr.io/source-org/ocm//ocm.software/mycomponent:1.0.0"
	src.CharLimit = 512
	src.Width = 80
	src.Focus()

	tgt := textinput.New()
	tgt.Placeholder = "ghcr.io/target-org/ocm"
	tgt.CharLimit = 512
	tgt.Width = 80

	return Model{
		executor:    executor,
		keys:        DefaultKeyMap(),
		step:        stepSource,
		sourceInput: src,
		targetInput: tgt,
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		for _, inp := range []*textinput.Model{&m.sourceInput, &m.targetInput} {
			if inp.Width > msg.Width-4 {
				inp.Width = msg.Width - 4
			}
		}
		if m.step == stepReview {
			m.reviewView.Width = msg.Width - 4
			m.reviewView.Height = msg.Height - 8
		}
		return m, nil

	case graphBuiltMsg:
		m.tgd = msg.tgd
		m.step = stepReview
		m.err = nil
		rendered, _ := yaml.Marshal(msg.tgd)
		m.reviewView = viewport.New(m.width-4, m.height-8)
		m.reviewView.SetContent(string(rendered))
		return m, nil

	case graphErrMsg:
		m.err = msg.err
		m.step = stepOptions
		return m, nil

	case transferProgressMsg:
		if msg.progress.IsLog {
			m.progressLog = append(m.progressLog, "  "+msg.progress.Step)
		} else {
			m.progressLog = append(m.progressLog, msg.progress.Step)
			m.progressCurrent = msg.progress.Current
			m.progressTotal = msg.progress.Total
		}
		// Chain: wait for the next progress message.
		return m, waitForProgress(m.progressCh, m.doneCh)

	case transferDoneMsg:
		m.step = stepDone
		m.execErr = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward to active input or viewport.
	switch m.step {
	case stepSource:
		var cmd tea.Cmd
		m.sourceInput, cmd = m.sourceInput.Update(msg)
		return m, cmd
	case stepTarget:
		var cmd tea.Cmd
		m.targetInput, cmd = m.targetInput.Update(msg)
		return m, cmd
	case stepReview:
		var cmd tea.Cmd
		m.reviewView, cmd = m.reviewView.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.step {
	case stepSource:
		switch msg.Type {
		case tea.KeyEnter:
			val := strings.TrimSpace(m.sourceInput.Value())
			if val == "" {
				return m, nil
			}
			m.step = stepTarget
			m.sourceInput.Blur()
			m.targetInput.Focus()
			return m, textinput.Blink
		case tea.KeyEsc:
			return m, nil // handled by app (quit)
		}
		var cmd tea.Cmd
		m.sourceInput, cmd = m.sourceInput.Update(msg)
		return m, cmd

	case stepTarget:
		switch msg.Type {
		case tea.KeyEnter:
			val := strings.TrimSpace(m.targetInput.Value())
			if val == "" {
				return m, nil
			}
			m.step = stepOptions
			m.targetInput.Blur()
			return m, nil
		case tea.KeyEsc:
			m.step = stepSource
			m.targetInput.Blur()
			m.sourceInput.Focus()
			return m, textinput.Blink
		}
		var cmd tea.Cmd
		m.targetInput, cmd = m.targetInput.Update(msg)
		return m, cmd

	case stepOptions:
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.optionCursor > 0 {
				m.optionCursor--
			}
		case key.Matches(msg, m.keys.Down):
			if m.optionCursor < 3 { // 0=recursive, 1=copy, 2=uploadAs, 3=build
				m.optionCursor++
			}
		case msg.Type == tea.KeySpace:
			switch m.optionCursor {
			case 0:
				m.recursive = !m.recursive
			case 1:
				m.copyResources = !m.copyResources
			case 2:
				m.uploadAs = (m.uploadAs + 1) % len(uploadAsLabels)
			}
		case msg.Type == tea.KeyEnter:
			if m.optionCursor == 3 {
				// Build graph
				m.err = nil
				return m, m.buildGraph()
			}
			// On option rows, enter also toggles/cycles
			switch m.optionCursor {
			case 0:
				m.recursive = !m.recursive
			case 1:
				m.copyResources = !m.copyResources
			case 2:
				m.uploadAs = (m.uploadAs + 1) % len(uploadAsLabels)
			}
		case msg.Type == tea.KeyEsc:
			m.step = stepTarget
			m.targetInput.Focus()
			return m, textinput.Blink
		}
		return m, nil

	case stepReview:
		switch {
		case key.Matches(msg, m.keys.Submit):
			m.step = stepExecuting
			progressCh := make(chan fetch.TransferProgress, 16)
			doneCh := make(chan error, 1)
			m.progressCh = progressCh
			m.doneCh = doneCh
			tgd := m.tgd
			executor := m.executor
			go func() {
				err := executor.Execute(context.Background(), tgd, progressCh)
				doneCh <- err
			}()
			return m, waitForProgress(progressCh, doneCh)
		case msg.Type == tea.KeyEsc:
			m.step = stepOptions
			return m, nil
		}
		var cmd tea.Cmd
		m.reviewView, cmd = m.reviewView.Update(msg)
		return m, cmd

	case stepDone:
		// Any key goes back to the app.
		return m, nil
	}

	return m, nil
}

func (m Model) buildGraph() tea.Cmd {
	source := strings.TrimSpace(m.sourceInput.Value())
	target := strings.TrimSpace(m.targetInput.Value())
	opts := fetch.TransferOptions{
		Recursive:     m.recursive,
		CopyResources: m.copyResources,
		UploadAs:      uploadAsLabels[m.uploadAs],
	}
	executor := m.executor
	return func() tea.Msg {
		tgd, err := executor.BuildGraph(context.Background(), source, target, opts)
		if err != nil {
			return graphErrMsg{err}
		}
		return graphBuiltMsg{tgd: tgd}
	}
}

// waitForProgress returns a tea.Cmd that blocks until either a progress
// message arrives or the transfer completes.
func waitForProgress(progressCh <-chan fetch.TransferProgress, doneCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case p, ok := <-progressCh:
			if !ok {
				// Channel closed — read the done result.
				err := <-doneCh
				return transferDoneMsg{err: err}
			}
			return transferProgressMsg{progress: p}
		case err := <-doneCh:
			// Drain remaining progress messages.
			for range progressCh {
			}
			return transferDoneMsg{err: err}
		}
	}
}

func (m Model) View() string {
	switch m.step {
	case stepSource:
		return m.viewInput("Step 1/4: Source Component", "Enter the source component reference:", m.sourceInput)
	case stepTarget:
		return m.viewInput("Step 2/4: Target Repository", "Enter the target repository:", m.targetInput)
	case stepOptions:
		return m.viewOptions()
	case stepReview:
		return m.viewReview()
	case stepExecuting:
		return m.viewExecuting()
	case stepDone:
		return m.viewDone()
	}
	return ""
}

func (m Model) viewInput(title, subtitle string, input textinput.Model) string {
	titleStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}).
		MarginBottom(1)
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"}).
		MarginBottom(1)
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
		MarginTop(1)
	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#FF4444"}).
		MarginTop(1)

	var sections []string
	sections = append(sections, titleStyle.Render(title))
	sections = append(sections, subtitleStyle.Render(subtitle))
	sections = append(sections, input.View())
	if m.err != nil {
		sections = append(sections, errStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}
	sections = append(sections, helpStyle.Render("enter: next  esc: back"))

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) viewOptions() string {
	titleStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}).
		MarginBottom(1)
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
		MarginTop(1)
	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#FF4444"}).
		MarginTop(1)

	var sections []string
	sections = append(sections, titleStyle.Render("Step 3/4: Transfer Options"))
	sections = append(sections, "")

	type optionRow struct {
		label   string
		value   string
		toggle  bool
		checked bool
		action  bool
	}
	options := []optionRow{
		{"Recursive", "", true, m.recursive, false},
		{"Copy all resources", "", true, m.copyResources, false},
		{"Upload as", uploadAsLabels[m.uploadAs], false, false, false},
		{"Build graph >>>", "", false, false, true},
	}

	for i, opt := range options {
		cursor := "  "
		if i == m.optionCursor {
			cursor = "> "
		}

		var line string
		switch {
		case opt.action:
			line = fmt.Sprintf("%s%s", cursor, opt.label)
		case opt.toggle:
			check := "[ ]"
			if opt.checked {
				check = "[x]"
			}
			line = fmt.Sprintf("%s%s %s", cursor, check, opt.label)
		default:
			line = fmt.Sprintf("%s    %s: %s", cursor, opt.label, opt.value)
		}

		if i == m.optionCursor {
			line = lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}).
				Render(line)
		}
		sections = append(sections, line)
	}

	sections = append(sections, "")
	if m.err != nil {
		sections = append(sections, errStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}
	sections = append(sections, helpStyle.Render("space/enter: toggle  j/k: navigate  esc: back"))

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) viewReview() string {
	titleStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}).
		MarginBottom(1)
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
		MarginTop(1)

	var sections []string
	sections = append(sections, titleStyle.Render("Step 4/4: Review Transformation Graph"))

	count := 0
	if m.tgd != nil {
		count = len(m.tgd.Transformations)
	}
	sections = append(sections, fmt.Sprintf("%d transformations will be executed:", count))
	sections = append(sections, "")
	sections = append(sections, m.reviewView.View())
	sections = append(sections, helpStyle.Render("enter: execute  esc: back  j/k: scroll"))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m Model) viewExecuting() string {
	titleStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"})
	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
	runningStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#0077CC", Dark: "#55AAFF"})
	doneStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#00AA00", Dark: "#44FF44"})
	failStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#FF4444"})

	var sections []string

	progressText := ""
	if m.progressTotal > 0 {
		progressText = fmt.Sprintf(" (%d/%d)", m.progressCurrent, m.progressTotal)
	}
	sections = append(sections, titleStyle.Render("Transferring..."+progressText))
	sections = append(sections, "")

	// Show all log entries, styled by state
	maxVisible := m.height - 5
	if maxVisible < 1 {
		maxVisible = 1
	}
	start := 0
	if len(m.progressLog) > maxVisible {
		start = len(m.progressLog) - maxVisible
	}
	for _, entry := range m.progressLog[start:] {
		var styled string
		switch {
		case strings.Contains(entry, "completed"):
			styled = doneStyle.Render("  " + entry)
		case strings.Contains(entry, "failed"):
			styled = failStyle.Render("  " + entry)
		case strings.Contains(entry, "running"):
			styled = runningStyle.Render("  " + entry)
		default:
			styled = dimStyle.Render("  " + entry)
		}
		sections = append(sections, styled)
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m Model) viewDone() string {
	titleStyle := lipgloss.NewStyle().Bold(true)
	doneStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#00AA00", Dark: "#44FF44"})
	failStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#FF4444"})
	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
		MarginTop(1)

	var sections []string

	if m.execErr != nil {
		sections = append(sections, titleStyle.
			Foreground(lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#FF4444"}).
			Render("Transfer failed"))
		sections = append(sections, fmt.Sprintf("\n%v", m.execErr))
	} else {
		sections = append(sections, titleStyle.
			Foreground(lipgloss.AdaptiveColor{Light: "#00AA00", Dark: "#44FF44"}).
			Render("Transfer completed successfully"))
	}
	sections = append(sections, "")

	// Show full log
	maxVisible := m.height - 6
	if maxVisible < 1 {
		maxVisible = 1
	}
	start := 0
	if len(m.progressLog) > maxVisible {
		start = len(m.progressLog) - maxVisible
	}
	for _, entry := range m.progressLog[start:] {
		var styled string
		switch {
		case strings.Contains(entry, "completed"):
			styled = doneStyle.Render("  " + entry)
		case strings.Contains(entry, "failed"):
			styled = failStyle.Render("  " + entry)
		default:
			styled = dimStyle.Render("  " + entry)
		}
		sections = append(sections, styled)
	}

	sections = append(sections, helpStyle.Render("press any key to continue"))

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

// Step returns the current wizard step.
func (m Model) Step() step {
	return m.step
}

// IsDone returns true when the transfer is complete.
func (m Model) IsDone() bool {
	return m.step == stepDone
}
