package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"ocm.software/open-component-model/bindings/go/tui/components"
	"ocm.software/open-component-model/bindings/go/tui/explorer"
	"ocm.software/open-component-model/bindings/go/tui/fetch"
	"ocm.software/open-component-model/bindings/go/tui/transfer"
)

// viewMode tracks which screen the app is showing.
type viewMode int

const (
	viewMenu     viewMode = iota // main menu
	viewInput                    // text input for reference (explorer)
	viewExplorer                 // component tree explorer
	viewTransfer                 // transfer wizard
)

// Config holds everything needed to initialize the TUI application.
type Config struct {
	FetcherFactory   fetch.FetcherFactory
	TransferExecutor fetch.TransferExecutor // optional, nil disables transfer
}

// fetcherReadyMsg is sent when the FetcherFactory succeeds.
type fetcherReadyMsg struct {
	fetcher   fetch.ComponentFetcher
	component string
	version   string
	reference string
}

// fetcherErrMsg is sent when the FetcherFactory fails.
type fetcherErrMsg struct{ err error }

// App is the root bubbletea model.
type App struct {
	config Config
	keys   KeyMap
	mode   viewMode

	// Menu
	menuCursor int

	// Input mode (explorer)
	input    textinput.Model
	inputErr error
	loading  bool

	// Explorer mode
	explorer  explorer.Model
	reference string

	// Transfer mode
	transfer transfer.Model

	// Layout
	width  int
	height int
	ready  bool
}

// NewApp creates the root TUI application model.
func NewApp(cfg Config) App {
	ti := textinput.New()
	ti.Placeholder = "ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.23.0"
	ti.CharLimit = 512
	ti.Width = 80

	app := App{
		config: cfg,
		keys:   DefaultKeyMap(),
		mode:   viewMenu,
		input:  ti,
	}

	if cfg.TransferExecutor != nil {
		app.transfer = transfer.New(cfg.TransferExecutor)
	}

	return app
}

func (a App) Init() tea.Cmd {
	return nil
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		if a.input.Width > msg.Width-4 {
			a.input.Width = msg.Width - 4
		}

		switch a.mode {
		case viewExplorer:
			explorerMsg := tea.WindowSizeMsg{Width: msg.Width, Height: msg.Height - 2}
			var cmd tea.Cmd
			a.explorer, cmd = a.explorer.Update(explorerMsg)
			return a, cmd
		case viewTransfer:
			var cmd tea.Cmd
			a.transfer, cmd = a.transfer.Update(msg)
			return a, cmd
		}
		return a, nil

	case fetcherReadyMsg:
		a.loading = false
		a.reference = msg.reference
		a.explorer = explorer.New(msg.fetcher, msg.component, msg.version)
		a.mode = viewExplorer
		explorerMsg := tea.WindowSizeMsg{Width: a.width, Height: a.height - 2}
		var sizeCmd tea.Cmd
		a.explorer, sizeCmd = a.explorer.Update(explorerMsg)
		initCmd := a.explorer.Init()
		return a, tea.Batch(sizeCmd, initCmd)

	case fetcherErrMsg:
		a.loading = false
		a.inputErr = msg.err
		a.input.Focus()
		return a, textinput.Blink

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, a.keys.Quit):
			switch a.mode {
			case viewMenu:
				return a, tea.Quit
			case viewInput:
				if msg.String() == "ctrl+c" {
					return a, tea.Quit
				}
				// esc goes back to menu
				if msg.String() == "q" {
					// 'q' is valid text input, don't quit
				} else {
					a.mode = viewMenu
					a.input.Blur()
					return a, nil
				}
			case viewExplorer:
				a.mode = viewMenu
				return a, nil
			case viewTransfer:
				if msg.String() == "ctrl+c" {
					return a, tea.Quit
				}
				// let transfer handle esc for going back between steps
			}

		case key.Matches(msg, a.keys.Tab):
			if a.mode == viewExplorer {
				a.explorer.ToggleFocus()
				return a, nil
			}
		}
	}

	switch a.mode {
	case viewMenu:
		return a.updateMenu(msg)
	case viewInput:
		return a.updateInput(msg)
	case viewExplorer:
		var cmd tea.Cmd
		a.explorer, cmd = a.explorer.Update(msg)
		return a, cmd
	case viewTransfer:
		return a.updateTransfer(msg)
	}

	return a, nil
}

func (a App) updateMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return a, nil
	}

	menuItems := a.menuItems()

	switch keyMsg.Type {
	case tea.KeyUp:
		if a.menuCursor > 0 {
			a.menuCursor--
		}
	case tea.KeyDown:
		if a.menuCursor < len(menuItems)-1 {
			a.menuCursor++
		}
	case tea.KeyEnter:
		switch menuItems[a.menuCursor] {
		case "Explore components":
			a.mode = viewInput
			a.input.Focus()
			a.inputErr = nil
			return a, textinput.Blink
		case "Transfer component versions":
			a.mode = viewTransfer
			a.transfer = transfer.New(a.config.TransferExecutor)
			return a, a.transfer.Init()
		}
	}

	// Also handle j/k for vim users
	switch keyMsg.String() {
	case "j":
		if a.menuCursor < len(menuItems)-1 {
			a.menuCursor++
		}
	case "k":
		if a.menuCursor > 0 {
			a.menuCursor--
		}
	}

	return a, nil
}

func (a App) menuItems() []string {
	items := []string{"Explore components"}
	if a.config.TransferExecutor != nil {
		items = append(items, "Transfer component versions")
	}
	return items
}

func (a App) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyEnter:
			ref := strings.TrimSpace(a.input.Value())
			if ref == "" {
				return a, nil
			}
			a.loading = true
			a.inputErr = nil
			a.input.Blur()
			return a, a.connectToRepo(ref)
		case tea.KeyEsc:
			a.mode = viewMenu
			a.input.Blur()
			return a, nil
		}
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	return a, cmd
}

func (a App) updateTransfer(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If transfer wizard is done and user presses a key, go back to menu
	if a.transfer.IsDone() {
		if _, ok := msg.(tea.KeyMsg); ok {
			a.mode = viewMenu
			return a, nil
		}
	}

	// Handle esc at source step to go back to menu
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEsc {
		// The transfer model returns m, nil for esc at stepSource.
		// We intercept that to go back to menu.
		prevStep := a.transfer.Step()
		var cmd tea.Cmd
		a.transfer, cmd = a.transfer.Update(msg)
		if a.transfer.Step() == prevStep && prevStep == 0 {
			// Still at source after esc → go back to menu
			a.mode = viewMenu
			return a, nil
		}
		return a, cmd
	}

	var cmd tea.Cmd
	a.transfer, cmd = a.transfer.Update(msg)
	return a, cmd
}

func (a App) connectToRepo(reference string) tea.Cmd {
	factory := a.config.FetcherFactory
	return func() tea.Msg {
		fetcher, component, version, err := factory(context.Background(), reference)
		if err != nil {
			return fetcherErrMsg{fmt.Errorf("connecting to %s: %w", reference, err)}
		}
		return fetcherReadyMsg{
			fetcher:   fetcher,
			component: component,
			version:   version,
			reference: reference,
		}
	}
}

func (a App) View() string {
	switch a.mode {
	case viewMenu:
		return a.viewMenu()
	case viewInput:
		return a.viewInput()
	case viewExplorer:
		return a.viewExplorer()
	case viewTransfer:
		return a.viewTransfer()
	}
	return ""
}

func (a App) viewMenu() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}).
		MarginBottom(2)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
		MarginTop(2)

	var sections []string
	sections = append(sections, titleStyle.Render("OCM TUI"))

	menuItems := a.menuItems()
	for i, item := range menuItems {
		cursor := "  "
		if i == a.menuCursor {
			cursor = "> "
		}
		line := cursor + item
		if i == a.menuCursor {
			line = lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}).
				Render(line)
		}
		sections = append(sections, line)
	}

	sections = append(sections, helpStyle.Render("j/k: navigate  enter: select  q: quit"))

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, content)
}

func (a App) viewInput() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}).
		MarginBottom(1)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"}).
		MarginBottom(1)

	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#FF4444"}).
		MarginTop(1)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
		MarginTop(1)

	var sections []string
	sections = append(sections, titleStyle.Render("Explore Components"))
	sections = append(sections, subtitleStyle.Render("Enter a component reference:"))
	sections = append(sections, a.input.View())

	if a.loading {
		sections = append(sections, subtitleStyle.Render("Connecting..."))
	}

	if a.inputErr != nil {
		sections = append(sections, errStyle.Render(fmt.Sprintf("Error: %v", a.inputErr)))
	}

	sections = append(sections, helpStyle.Render("enter: connect  esc: back"))

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, content)
}

func (a App) viewExplorer() string {
	statusBar := components.StatusBar(a.width, "ocm tui", a.reference)

	separator := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"}).
		Render(repeatChar("─", a.width))

	explorerView := a.explorer.View()

	return lipgloss.JoinVertical(lipgloss.Left,
		statusBar,
		separator,
		explorerView,
	)
}

func (a App) viewTransfer() string {
	return a.transfer.View()
}

func repeatChar(ch string, count int) string {
	if count <= 0 {
		return ""
	}
	result := make([]byte, 0, count*len(ch))
	for i := 0; i < count; i++ {
		result = append(result, ch...)
	}
	return string(result)
}
