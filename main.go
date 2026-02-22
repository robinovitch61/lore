package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/robinovitch61/viewport/filterableviewport"
	"github.com/robinovitch61/viewport/viewport"
	"github.com/robinovitch61/viewport/viewport/item"
)

var (
	lineNumberStyleEven = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	lineNumberStyleOdd  = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Background(lipgloss.Color("#262626"))
)

type object struct {
	lineNumber item.SingleItem
	content    item.SingleItem
}

func (o object) GetItem() item.Item {
	// pin the first item (line number) so it stays visible during horizontal panning.
	return item.NewMultiWithPinned(1, o.lineNumber, o.content)
}

type appKeys struct {
	quit                     key.Binding
	toggleShowLineNumbersKey key.Binding
	toggleWrapTextKey        key.Binding
	toggleSelectionKey       key.Binding
}

var appKeyMap = appKeys{
	quit: key.NewBinding(
		key.WithKeys("ctrl+c", "q"),
		key.WithHelp("ctrl+c/q", "quit"),
	),
	toggleShowLineNumbersKey: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "toggle line numbers"),
	),
	toggleWrapTextKey: key.NewBinding(
		key.WithKeys("w"),
		key.WithHelp("w", "toggle wrapping"),
	),
	toggleSelectionKey: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "toggle selection"),
	),
}

var viewportKeyMap = viewport.DefaultKeyMap()
var filterableViewportKeyMap = filterableviewport.DefaultKeyMap()
var styles = filterableviewport.DefaultStyles()

type newLinesMsg struct {
	lines []string
}

type stdinDoneMsg struct{}

type model struct {
	itemNumber                    int
	showLineNumbers               bool
	objects                       []object
	fv                            *filterableviewport.Model[object]
	ready                         bool
	viewportWidth, viewportHeight int
	bufferedLines                 []string // lines that arrived before viewport was ready
}

func stdinIsPipe() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) == 0
}

// convertOverstrike converts backspace-based overstrike formatting (used by e.g. man pages)
// to ANSI escape codes. Patterns:
//   - c\x08c (char, backspace, same char) -> bold
//   - _\x08c (underscore, backspace, char) -> underline
func convertOverstrike(s string) string {
	if !strings.Contains(s, "\b") {
		return s
	}

	var result strings.Builder
	result.Grow(len(s))

	const (
		styleNone = iota
		styleBold
		styleUnderline
	)

	currentStyle := styleNone
	i := 0

	for i < len(s) {
		// decode current rune
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// invalid UTF-8, just copy the byte
			result.WriteByte(s[i])
			i++
			continue
		}

		// check if next char is backspace and there's a char after that
		if i+size < len(s) && s[i+size] == '\b' && i+size+1 < len(s) {
			// decode the character after the backspace
			nextR, nextSize := utf8.DecodeRuneInString(s[i+size+1:])

			if r == '_' && nextR != '_' {
				// underscore + backspace + char = underline
				if currentStyle != styleUnderline {
					if currentStyle != styleNone {
						result.WriteString("\x1b[0m")
					}
					result.WriteString("\x1b[4m")
					currentStyle = styleUnderline
				}
				result.WriteRune(nextR)
				i += size + 1 + nextSize // skip: underscore, backspace, char
				continue
			} else if r == nextR {
				// char + backspace + same char = bold
				if currentStyle != styleBold {
					if currentStyle != styleNone {
						result.WriteString("\x1b[0m")
					}
					result.WriteString("\x1b[1m")
					currentStyle = styleBold
				}
				result.WriteRune(r)
				i += size + 1 + nextSize // skip: char, backspace, char
				continue
			}
		}

		// no overstrike pattern, reset style if needed and output char
		if currentStyle != styleNone {
			result.WriteString("\x1b[0m")
			currentStyle = styleNone
		}

		// skip lone backspaces
		if r == '\b' {
			i += size
			continue
		}

		result.WriteRune(r)
		i += size
	}

	// close any open style
	if currentStyle != styleNone {
		result.WriteString("\x1b[0m")
	}

	return result.String()
}

func linesToObjects(lines []string) []object {
	objects := make([]object, len(lines))
	for i, line := range lines {
		objects[i] = object{
			lineNumber: item.NewItem(""),
			content:    item.NewItem(convertOverstrike(line)),
		}
	}
	return objects
}

func (m model) Init() (tea.Model, tea.Cmd) {
	if readingFromInput {
		return m, readInputCmd()
	}
	return m, nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case newLinesMsg:
		if m.ready {
			// viewport is ready, process lines immediately
			if len(msg.lines) > 0 {
				newObjects := linesToObjects(msg.lines)
				m.objects = append(m.objects, newObjects...)
				m.fv.AppendObjects(newObjects)
				m.itemNumber += len(msg.lines)
			}
		} else {
			// viewport not ready yet, buffer the lines
			m.bufferedLines = append(m.bufferedLines, msg.lines...)
		}
		if readingFromInput {
			return m, readInputCmd()
		}
		return m, nil

	case stdinDoneMsg:
		// exit if EOF with no content
		if len(m.objects) == 0 && len(m.bufferedLines) == 0 {
			return m, tea.Quit
		}
		return m, nil

	case tea.KeyMsg:
		// always allow ctrl+c to quit
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if !m.ready {
			return m, nil
		}

		if m.fv.IsCapturingInput() {
			m.fv, cmd = m.fv.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

		// only allow 'q' to quit when not capturing input
		if key.Matches(msg, appKeyMap.quit) {
			return m, tea.Quit
		}

		switch {
		case key.Matches(msg, appKeyMap.toggleShowLineNumbersKey):
			m.showLineNumbers = !m.showLineNumbers
			for i := range m.objects {
				lineNum := ""
				if m.showLineNumbers {
					num := fmt.Sprintf("%d ", i+1)
					// alternate background color for each line
					if (i+1)%2 == 1 {
						lineNum = lineNumberStyleOdd.Render(num)
					} else {
						lineNum = lineNumberStyleEven.Render(num)
					}
				}
				m.objects[i].lineNumber = item.NewItem(lineNum)
			}
			m.fv.SetObjects(m.objects)
			return m, nil
		case key.Matches(msg, appKeyMap.toggleWrapTextKey):
			m.fv.SetWrapText(!m.fv.GetWrapText())
			return m, nil
		case key.Matches(msg, appKeyMap.toggleSelectionKey):
			m.fv.SetSelectionEnabled(!m.fv.GetSelectionEnabled())
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.viewportWidth, m.viewportHeight = msg.Width, msg.Height
		if !m.ready {
			// configure file saving
			homeDir, _ := os.UserHomeDir()
			saveDir := filepath.Join(homeDir, ".lore", "saved")
			saveKey := key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save"))

			vp := viewport.New[object](
				m.viewportWidth,
				m.viewportHeight,
				viewport.WithKeyMap[object](viewportKeyMap),
				viewport.WithStyles[object](viewport.DefaultStyles()),
				viewport.WithStickyBottom[object](false),
				viewport.WithFileSaving[object](saveDir, saveKey),
			)
			m.fv = filterableviewport.New[object](
				vp,
				filterableviewport.WithKeyMap[object](filterableViewportKeyMap),
				filterableviewport.WithStyles[object](styles),
				filterableviewport.WithPrefixText[object]("Filter:"),
				filterableviewport.WithEmptyText[object](""),
				filterableviewport.WithMatchingItemsOnly[object](false),
				filterableviewport.WithCanToggleMatchingItemsOnly[object](true),
				filterableviewport.WithHorizontalPad[object](50),
				filterableviewport.WithVerticalPad[object](20),
			)
			m.fv.SetWrapText(false)
			m.fv.SetSelectionEnabled(false)
			m.ready = true
		} else {
			m.fv.SetWidth(m.viewportWidth)
			m.fv.SetHeight(m.viewportHeight)
		}
	}

	if m.ready {
		m.fv, cmd = m.fv.Update(msg)
		cmds = append(cmds, cmd)

		// after viewport is fully initialized, process any buffered lines
		if _, ok := msg.(tea.WindowSizeMsg); ok && len(m.bufferedLines) > 0 {
			newObjects := linesToObjects(m.bufferedLines)
			m.objects = append(m.objects, newObjects...)
			m.fv.SetObjects(m.objects)
			m.itemNumber += len(m.bufferedLines)
			m.bufferedLines = nil
		}
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "Loading..."
	}
	return m.fv.View()
}

var inputReader *bufio.Reader
var readingFromInput bool

// readInputCmd reads lines from the input source in batches and returns them as a message
func readInputCmd() tea.Cmd {
	return func() tea.Msg {
		const maxBatchSize = 500
		lines := make([]string, 0, maxBatchSize)

		for range maxBatchSize {
			line, err := inputReader.ReadString('\n')
			if err == io.EOF {
				if len(line) > 0 {
					lines = append(lines, strings.TrimSuffix(line, "\n"))
				}
				if len(lines) > 0 {
					return newLinesMsg{lines: lines}
				}
				return stdinDoneMsg{}
			}
			if err != nil {
				if len(lines) > 0 {
					return newLinesMsg{lines: lines}
				}
				return stdinDoneMsg{}
			}
			lines = append(lines, strings.TrimSuffix(line, "\n"))

			// check if more data is immediately available, otherwise return what we have
			// this prevents blocking when data arrives slowly
			if inputReader.Buffered() == 0 {
				break
			}
		}

		if len(lines) > 0 {
			return newLinesMsg{lines: lines}
		}
		return stdinDoneMsg{}
	}
}

func main() {
	var opts []tea.ProgramOption
	opts = append(opts, tea.WithAltScreen())

	if len(os.Args) > 1 {
		// file argument provided
		filename := filepath.Clean(os.Args[1])
		file, err := os.Open(filename) //nolint:gosec // intentional user-provided file path
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening file: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = file.Close() }()
		inputReader = bufio.NewReader(file)
		readingFromInput = true
	} else if stdinIsPipe() {
		// reading from piped stdin, need /dev/tty for keyboard input
		tty, err := os.Open("/dev/tty")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening /dev/tty: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = tty.Close() }()
		opts = append(opts, tea.WithInput(tty))
		inputReader = bufio.NewReader(os.Stdin)
		readingFromInput = true
	} else {
		fmt.Fprintf(os.Stderr, "usage: lore <file> or command | lore\n")
		os.Exit(1)
	}

	p := tea.NewProgram(model{}, opts...)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running program: %v\n", err)
		os.Exit(1)
	}
}
