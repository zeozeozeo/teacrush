package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	appStyle = lipgloss.NewStyle().Margin(1, 2)

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFF")).
			Background(lipgloss.Color("#5865F2")).
			Padding(0, 1).
			Bold(true)

	stepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#5865F2")).Bold(true)
	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
	doneStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true)

	selectedItemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	itemStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	progressFullStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#5865F2"))
	progressEmptyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

type state int

const (
	stateInputFile state = iota
	stateInputSize
	stateSelectHW
	stateSelectCodec
	stateProcessing
	stateDone
	stateError
)

type hwType string

const (
	hwCPU    hwType = "CPU (Software, Best Quality)"
	hwNVIDIA hwType = "NVIDIA (NVENC)"
	hwAMD    hwType = "AMD (AMF)"
	hwINTEL  hwType = "Intel (QSV)"
)

var hardwareOptions = []hwType{hwCPU, hwNVIDIA, hwAMD, hwINTEL}

type codecInfo struct {
	Name      string
	FFmpegLib string
	Ext       string
}

var encoderMap = map[hwType][]codecInfo{
	hwCPU: {
		{"AV1 (Slowest, Best Quality)", "libaom-av1", ".webm"},
		{"VP9 (Medium Quality)", "libvpx-vp9", ".webm"},
		{"H.264 (Fast)", "libx264", ".mp4"},
		{"H.265 (High Efficiency)", "libx265", ".mp4"},
	},
	hwNVIDIA: {
		{"H.264 (NVENC)", "h264_nvenc", ".mp4"},
		{"HEVC (NVENC)", "hevc_nvenc", ".mp4"},
		{"AV1 (NVENC - RTX 40xx+)", "av1_nvenc", ".webm"},
	},
	hwAMD: {
		{"H.264 (AMF)", "h264_amf", ".mp4"},
		{"HEVC (AMF)", "hevc_amf", ".mp4"},
		{"AV1 (AMF - RX 7000+)", "av1_amf", ".webm"},
	},
	hwINTEL: {
		{"H.264 (QSV)", "h264_qsv", ".mp4"},
		{"HEVC (QSV)", "hevc_qsv", ".mp4"},
		{"VP9 (QSV)", "vp9_qsv", ".webm"},
		{"AV1 (QSV - Arc GPU)", "av1_qsv", ".webm"},
	},
}

type progressMsg struct {
	line     string
	progress float64
}

type workDoneMsg struct {
	outputFile string
	finalSize  string
	err        error
}

type model struct {
	state     state
	textInput textinput.Model
	spinner   spinner.Model
	err       error

	filePath      string
	targetSizeMB  float64
	selectedHW    int
	selectedCodec int

	progressChan chan progressMsg
	currentLog   string
	percent      float64
	outputFile   string
	finalSize    string

	suggestions   []string
	suggestionIdx int
}

func initialModel() model {
	ti := textinput.New()
	ti.CharLimit = 1000
	ti.Width = 60
	ti.Focus()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	m := model{
		state:      stateInputFile,
		spinner:    s,
		selectedHW: 0,
	}

	if len(os.Args) > 1 {
		argPath := cleanPath(os.Args[1])
		if _, err := os.Stat(argPath); err == nil {
			m.filePath = argPath
			m.state = stateInputSize
			ti.Placeholder = "e.g. 10 (for 10MB)"
		} else {
			ti.Placeholder = "Drag & Drop video here..."
		}
	} else {
		ti.Placeholder = "Drag & Drop video here..."
	}

	m.textInput = ti
	return m
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}

		switch m.state {
		case stateInputFile:
			if msg.Type == tea.KeyTab {
				input := m.textInput.Value()

				if len(m.suggestions) > 0 && input == m.suggestions[m.suggestionIdx] {
					m.suggestionIdx = (m.suggestionIdx + 1) % len(m.suggestions)
				} else {
					m.suggestions = findMatches(input)
					m.suggestionIdx = 0
				}

				if len(m.suggestions) > 0 {
					choice := m.suggestions[m.suggestionIdx]
					m.textInput.SetValue(choice)
					m.textInput.SetCursor(len(choice))
				}
				return m, nil
			}

			if msg.Type == tea.KeyEnter {
				path := cleanPath(m.textInput.Value())
				if _, err := os.Stat(path); err != nil {
					m.err = fmt.Errorf("file not found: %s", path)
				} else {
					m.filePath = path
					m.state = stateInputSize
					m.textInput.Reset()
					m.textInput.Placeholder = "e.g. 10 (for 10MB)"
					m.err = nil
				}
			}

		case stateInputSize:
			if msg.Type == tea.KeyEnter {
				size, err := strconv.ParseFloat(m.textInput.Value(), 64)
				if err != nil || size <= 0 {
					m.err = fmt.Errorf("invalid size")
				} else {
					m.targetSizeMB = size
					m.state = stateSelectHW
					m.err = nil
				}
			}

		case stateSelectHW:
			switch msg.String() {
			case "up", "k":
				if m.selectedHW > 0 {
					m.selectedHW--
				}
			case "down", "j":
				if m.selectedHW < len(hardwareOptions)-1 {
					m.selectedHW++
				}
			case "enter":
				m.state = stateSelectCodec
				m.selectedCodec = 0
			}

		case stateSelectCodec:
			hw := hardwareOptions[m.selectedHW]
			options := encoderMap[hw]

			switch msg.String() {
			case "up", "k":
				if m.selectedCodec > 0 {
					m.selectedCodec--
				}
			case "down", "j":
				if m.selectedCodec < len(options)-1 {
					m.selectedCodec++
				}
			case "enter":
				m.state = stateProcessing
				m.progressChan = make(chan progressMsg)

				codecCfg := options[m.selectedCodec]

				return m, tea.Batch(
					m.spinner.Tick,
					startEncoding(m.filePath, m.targetSizeMB, hw, codecCfg, m.progressChan),
					waitForProgress(m.progressChan),
				)
			}
		}

	case progressMsg:
		m.currentLog = msg.line
		if msg.progress > 0 {
			m.percent = msg.progress
		}
		return m, waitForProgress(m.progressChan)

	case workDoneMsg:
		if msg.err != nil {
			m.state = stateError
			m.err = msg.err
		} else {
			m.state = stateDone
			m.outputFile = msg.outputFile
			m.finalSize = msg.finalSize
		}
		return m, tea.Quit

	case spinner.TickMsg:
		if m.state == stateProcessing {
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	if m.state == stateInputFile || m.state == stateInputSize {
		m.textInput, cmd = m.textInput.Update(msg)
	}

	return m, cmd
}

func (m model) View() string {
	var s strings.Builder

	s.WriteString(titleStyle.Render(" Teacrush "))
	s.WriteString("\n\n")

	if m.err != nil {
		s.WriteString(errStyle.Render(fmt.Sprintf("ERROR: %v", m.err)))
		s.WriteString("\n\n")
	}

	switch m.state {
	case stateInputFile:
		s.WriteString(stepStyle.Render("1. Select Video File"))
		s.WriteString("\nDrag & Drop file:\n\n")
		s.WriteString(m.textInput.View())

	case stateInputSize:
		s.WriteString(stepStyle.Render("2. Target Size"))
		s.WriteString(fmt.Sprintf("\nFile: %s", filepath.Base(m.filePath)))
		s.WriteString("\nMax MB (Audio+Video):\n\n")
		s.WriteString(m.textInput.View())

	case stateSelectHW:
		s.WriteString(stepStyle.Render("3. Select Hardware"))
		s.WriteString(fmt.Sprintf("\nTarget: %.2f MB\n\n", m.targetSizeMB))
		for i, hw := range hardwareOptions {
			cursor := "  "
			style := itemStyle
			if m.selectedHW == i {
				cursor = "> "
				style = selectedItemStyle
			}
			s.WriteString(style.Render(cursor+string(hw)) + "\n")
		}

	case stateSelectCodec:
		s.WriteString(stepStyle.Render("4. Select Codec"))
		hw := hardwareOptions[m.selectedHW]
		s.WriteString(fmt.Sprintf("\nHardware: %s\n\n", hw))

		options := encoderMap[hw]
		for i, c := range options {
			cursor := "  "
			style := itemStyle
			if m.selectedCodec == i {
				cursor = "> "
				style = selectedItemStyle
			}
			s.WriteString(style.Render(cursor+c.Name) + "\n")
		}

	case stateProcessing:
		s.WriteString(stepStyle.Render("Compressing..."))
		s.WriteString("\n\n")

		width := 40
		filled := int(math.Max(0, math.Min(float64(width), m.percent*float64(width))))
		bar := progressFullStyle.Render(strings.Repeat("█", filled)) +
			progressEmptyStyle.Render(strings.Repeat("░", width-filled))

		s.WriteString(fmt.Sprintf("%s %s  %.0f%%\n\n", m.spinner.View(), bar, m.percent*100))
		s.WriteString(lipgloss.NewStyle().Faint(true).Render("Status: " + m.currentLog))

	case stateDone:
		s.WriteString(doneStyle.Render("Success!"))
		s.WriteString(fmt.Sprintf("\n\nSaved to:\n%s", m.outputFile))
		s.WriteString(fmt.Sprintf("\n%s", m.finalSize))

	case stateError:
		s.WriteString(errStyle.Render("Failed."))
	}

	return appStyle.Render(s.String())
}

func waitForProgress(sub <-chan progressMsg) tea.Cmd {
	return func() tea.Msg {
		if msg, ok := <-sub; ok {
			return msg
		}
		return nil
	}
}

func startEncoding(inputFile string, targetMB float64, hw hwType, codecCfg codecInfo, progressChan chan progressMsg) tea.Cmd {
	return func() tea.Msg {
		defer close(progressChan)

		progressChan <- progressMsg{line: "Analyzing file...", progress: 0}
		info, err := probeFile(inputFile)
		if err != nil {
			return workDoneMsg{err: err}
		}

		duration, _ := strconv.ParseFloat(info.Format.Duration, 64)
		hasAudio := false
		for _, s := range info.Streams {
			if s.CodecType == "audio" {
				hasAudio = true
				break
			}
		}

		targetBits := targetMB * 8388608 // 8 * 1024 * 1024
		audioRate := 0.0
		if hasAudio {
			audioRate = 128 * 1024
		}

		totalRate := targetBits / duration
		videoRate := (totalRate - audioRate) * 0.95
		if videoRate < 50*1024 {
			videoRate = 50 * 1024
		}

		videoKBit := int(videoRate / 1024)

		dir := filepath.Dir(inputFile)
		name := strings.TrimSuffix(filepath.Base(inputFile), filepath.Ext(inputFile))
		outputFile := filepath.Join(dir, fmt.Sprintf("%s_compressed%s", name, codecCfg.Ext))

		isCPU := hw == hwCPU

		var audioArgs []string
		if hasAudio {
			if codecCfg.Ext == ".mp4" {
				audioArgs = []string{"-c:a", "aac", "-b:a", "128k"}
			} else {
				audioArgs = []string{"-c:a", "libopus", "-b:a", "128k"}
			}
		} else {
			audioArgs = []string{"-an"}
		}

		if isCPU {
			passLog := filepath.Join(os.TempDir(), fmt.Sprintf("pass_%d", time.Now().UnixNano()))

			extraArgs := []string{}
			switch codecCfg.FFmpegLib {
			case "libvpx-vp9":
				extraArgs = []string{"-speed", "4", "-row-mt", "1", "-tile-columns", "2"}
			case "libaom-av1":
				extraArgs = []string{"-cpu-used", "6", "-row-mt", "1", "-tiles", "2x2"}
			case "libx264":
				extraArgs = []string{"-preset", "slow"}
			default:
				extraArgs = []string{"-preset", "medium"}
			}

			// pass 1
			nullOut := "/dev/null"
			if runtime.GOOS == "windows" {
				nullOut = "NUL"
			}

			p1 := []string{"-y", "-i", inputFile, "-c:v", codecCfg.FFmpegLib, "-b:v", fmt.Sprintf("%dk", videoKBit), "-pass", "1", "-passlogfile", passLog, "-an"}
			p1 = append(p1, extraArgs...)
			p1 = append(p1, "-f", "null", nullOut)

			if err := runFFmpeg(p1, progressChan, duration, "Pass 1 (Analysis)"); err != nil {
				return workDoneMsg{err: err}
			}

			// pass 2
			p2 := []string{"-y", "-i", inputFile, "-c:v", codecCfg.FFmpegLib, "-b:v", fmt.Sprintf("%dk", videoKBit), "-pass", "2", "-passlogfile", passLog}
			p2 = append(p2, extraArgs...)
			p2 = append(p2, audioArgs...)
			p2 = append(p2, outputFile)

			if err := runFFmpeg(p2, progressChan, duration, "Pass 2 (Encoding)"); err != nil {
				return workDoneMsg{err: err}
			}
			_ = os.Remove(passLog + "-0.log")
			_ = os.Remove(passLog + ".log")
			_ = os.Remove(passLog + "-0.log.mbtree")
		} else {
			// gpu (cbr)
			extraArgs := []string{}

			if strings.Contains(codecCfg.FFmpegLib, "nvenc") {
				extraArgs = []string{"-preset", "p7", "-rc", "vbr", "-cq", "0"}
			} else if strings.Contains(codecCfg.FFmpegLib, "amf") {
				extraArgs = []string{"-quality", "quality"}
			} else if strings.Contains(codecCfg.FFmpegLib, "qsv") {
				extraArgs = []string{"-preset", "veryslow"}
			}

			cmdArgs := []string{
				"-y",
				"-hwaccel", "auto",
				"-i", inputFile,
				"-c:v", codecCfg.FFmpegLib,
				"-b:v", fmt.Sprintf("%dk", videoKBit),
				"-maxrate", fmt.Sprintf("%dk", videoKBit),
				"-bufsize", fmt.Sprintf("%dk", videoKBit*2),
			}
			cmdArgs = append(cmdArgs, extraArgs...)
			cmdArgs = append(cmdArgs, audioArgs...)
			cmdArgs = append(cmdArgs, outputFile)

			if err := runFFmpeg(cmdArgs, progressChan, duration, "GPU Encoding"); err != nil {
				return workDoneMsg{err: err}
			}
		}

		fi, err := os.Stat(outputFile)
		sizeStr := "Unknown"
		if err == nil {
			mb := float64(fi.Size()) / 1024 / 1024
			sizeStr = fmt.Sprintf("%.2f MB", mb)
		}

		return workDoneMsg{outputFile: outputFile, finalSize: sizeStr, err: nil}
	}
}

func runFFmpeg(args []string, ch chan<- progressMsg, totalDuration float64, prefix string) error {
	finalArgs := append([]string{"-hide_banner", "-nostats", "-progress", "pipe:1"}, args...)
	cmd := exec.Command("ffmpeg", finalArgs...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	startTime := time.Now()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "=")
		if len(parts) == 2 && parts[0] == "out_time_us" {
			us, _ := strconv.ParseFloat(parts[1], 64)
			cur := us / 1000000.0

			pct := 0.0
			if totalDuration > 0 {
				pct = cur / totalDuration
			}
			if pct > 1.0 {
				pct = 1.0
			}

			etaStr := "..."
			if pct > 0.01 {
				elapsed := time.Since(startTime).Seconds()
				remaining := (elapsed / pct) - elapsed
				if remaining < 0 {
					remaining = 0
				}

				remDur := time.Duration(remaining) * time.Second
				etaStr = fmt.Sprintf("eta %02d:%02d", int(remDur.Minutes()), int(remDur.Seconds())%60)
			}

			ch <- progressMsg{
				line:     fmt.Sprintf("%s (%s)", prefix, etaStr),
				progress: pct,
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%v\nLog: %s", err, stderr.String())
	}
	return nil
}

func cleanPath(path string) string {
	return strings.Trim(strings.TrimSpace(path), "\"'")
}

func findMatches(input string) []string {
	dir, file := filepath.Split(input)
	if dir == "" {
		dir = "."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var matches []string
	for _, e := range entries {
		if strings.HasPrefix(strings.ToLower(e.Name()), strings.ToLower(file)) {
			fullPath := filepath.Join(dir, e.Name())

			if dir == "." {
				fullPath = e.Name()
			}
			if e.IsDir() {
				fullPath += string(os.PathSeparator)
			}

			matches = append(matches, fullPath)
		}
	}
	return matches
}

type FFProbeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func probeFile(path string) (*FFProbeOutput, error) {
	out, err := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", path).Output()
	if err != nil {
		return nil, err
	}
	var info FFProbeOutput
	json.Unmarshal(out, &info)
	return &info, nil
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
