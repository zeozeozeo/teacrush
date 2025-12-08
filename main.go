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
	stateInputRes
	stateGifFPS
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

	isGifMode bool

	filePath      string
	targetSizeMB  float64
	targetRes     string
	targetFPS     string // empty = real
	trimStart     string
	trimEnd       string
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

func initialModel(gifMode bool) model {
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
		isGifMode:  gifMode,
	}

	args := os.Args[1:]
	skip := 0
	for i, arg := range args {
		if skip > 0 {
			skip--
			continue
		}
		if arg == "-gif" {
			continue
		}
		if arg == "-trim" {
			if i+2 < len(args) {
				m.trimStart = args[i+1]
				m.trimEnd = args[i+2]
				skip = 2
				continue
			}
		}

		clean := cleanPath(arg)
		if _, err := os.Stat(clean); err == nil {
			m.filePath = clean
			m.state = stateInputSize
			ti.Placeholder = "e.g. 10 (for 10MB)"
		}
	}

	if m.filePath == "" {
		ti.Placeholder = "Drag & Drop or enter path..."
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
				val := m.textInput.Value()
				if val == "" {
					val = "8"
				}
				size, err := strconv.ParseFloat(val, 64)
				if err != nil || size <= 0 {
					m.err = fmt.Errorf("invalid size")
				} else {
					m.targetSizeMB = size
					m.state = stateInputRes
					m.textInput.Reset()
					m.textInput.Placeholder = "Enter=Original, 2=Half-size, or e.g. 1280x720"
					m.err = nil
				}
			}

		case stateInputRes:
			if msg.Type == tea.KeyEnter {
				m.targetRes = m.textInput.Value()
				m.textInput.Reset()

				if m.isGifMode {
					m.state = stateGifFPS
					m.textInput.Placeholder = "Enter=Original, or e.g. 15"
				} else {
					m.state = stateSelectHW
					m.textInput.Blur()
				}
				m.err = nil
			}

		case stateGifFPS:
			if msg.Type == tea.KeyEnter {
				m.targetFPS = m.textInput.Value()
				m.textInput.Blur()

				m.state = stateProcessing
				m.progressChan = make(chan progressMsg)
				codecCfg := codecInfo{Name: "GIF", Ext: ".gif"}
				return m, tea.Batch(
					m.spinner.Tick,
					startEncoding(m.filePath, m.targetSizeMB, m.targetRes, m.targetFPS, m.trimStart, m.trimEnd, hwCPU, codecCfg, m.progressChan, true),
					waitForProgress(m.progressChan),
				)
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
					startEncoding(m.filePath, m.targetSizeMB, m.targetRes, "", m.trimStart, m.trimEnd, hw, codecCfg, m.progressChan, false),
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

	if m.state == stateInputFile || m.state == stateInputSize || m.state == stateInputRes || m.state == stateGifFPS {
		m.textInput, cmd = m.textInput.Update(msg)
	}

	return m, cmd
}

func (m model) View() string {
	var s strings.Builder

	s.WriteString(titleStyle.Render(" Teacrush "))
	if m.isGifMode {
		s.WriteString(" (GIF Mode)")
	}
	if m.trimStart != "" {
		s.WriteString(fmt.Sprintf(" [Trim: %s-%s]", m.trimStart, m.trimEnd))
	}
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
		if m.isGifMode {
			s.WriteString("\nMax MB (GIF), Empty=dontcare:\n\n")
		} else {
			s.WriteString("\nMax MB (Audio+Video), Empty=dontcare:\n\n")
		}
		s.WriteString(m.textInput.View())

	case stateInputRes:
		s.WriteString(stepStyle.Render("3. Target Resolution"))
		s.WriteString("\nLeave empty for original.")
		s.WriteString("\nType '2' for half size (1/2).")
		s.WriteString("\nType '1280x720' for fixed size.\n\n")
		s.WriteString(m.textInput.View())

	case stateGifFPS:
		s.WriteString(stepStyle.Render("4. GIF Framerate"))
		s.WriteString("\nLeave empty for original FPS.")
		s.WriteString("\nEnter a number (e.g. 15) to set an FPS limit.\n\n")
		s.WriteString(m.textInput.View())

	case stateSelectHW:
		s.WriteString(stepStyle.Render("4. Select Hardware"))
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
		s.WriteString(stepStyle.Render("5. Select Codec"))
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
		mode := "Compressing"
		if m.isGifMode {
			mode = "Creating GIF"
		}
		s.WriteString(stepStyle.Render(mode + "..."))
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

func buildScaleFilter(input string) string {
	input = strings.TrimSpace(input)
	if input == "" || input == "1" {
		return ""
	}
	if div, err := strconv.ParseFloat(input, 64); err == nil && div > 0 {
		return fmt.Sprintf("scale=trunc((iw/%g)/2)*2:trunc((ih/%g)/2)*2", div, div)
	}
	if strings.Contains(input, "x") || strings.Contains(input, ":") {
		formatted := strings.ReplaceAll(input, "x", ":")
		return fmt.Sprintf("scale=%s", formatted)
	}
	return ""
}

func parseDuration(s string) float64 {
	s = strings.TrimSuffix(s, "s")
	parts := strings.Split(s, ":")
	sec := 0.0
	mul := 1.0
	for i := len(parts) - 1; i >= 0; i-- {
		v, _ := strconv.ParseFloat(parts[i], 64)
		sec += v * mul
		mul *= 60
	}
	return sec
}

func startEncoding(inputFile string, targetMB float64, resInput string, fpsInput string, trimStart, trimEnd string, hw hwType, codecCfg codecInfo, progressChan chan progressMsg, isGif bool) tea.Cmd {
	return func() tea.Msg {
		defer close(progressChan)

		progressChan <- progressMsg{line: "Analyzing file...", progress: 0}
		info, err := probeFile(inputFile)
		if err != nil {
			return workDoneMsg{err: err}
		}

		duration, _ := strconv.ParseFloat(info.Format.Duration, 64)

		if trimStart != "" && trimEnd != "" {
			s := parseDuration(trimStart)
			e := parseDuration(trimEnd)
			if e > s {
				duration = e - s
			}
		}

		dir := filepath.Dir(inputFile)
		name := strings.TrimSuffix(filepath.Base(inputFile), filepath.Ext(inputFile))
		outputFile := filepath.Join(dir, fmt.Sprintf("%s_compressed%s", name, codecCfg.Ext))

		scaleFilter := buildScaleFilter(resInput)

		vfFilters := []string{}
		if scaleFilter != "" {
			vfFilters = append(vfFilters, scaleFilter)
		}
		vfFilters = append(vfFilters, "mpdecimate") // remove duplicate frames

		vfString := strings.Join(vfFilters, ",")

		trimArgs := []string{}
		if trimStart != "" && trimEnd != "" {
			trimArgs = []string{"-ss", trimStart, "-to", trimEnd}
		}

		// gif mode
		if isGif {
			gifVf := []string{}
			if scaleFilter != "" {
				gifVf = append(gifVf, scaleFilter)
			}
			gifVf = append(gifVf, "mpdecimate")

			if fpsInput != "" {
				gifVf = append(gifVf, fmt.Sprintf("fps=%s", fpsInput))
			}

			gifVfStr := strings.Join(gifVf, ",")

			paletteFile := filepath.Join(os.TempDir(), fmt.Sprintf("palette_%d.png", time.Now().UnixNano()))
			defer os.Remove(paletteFile)

			progressChan <- progressMsg{line: "Generating Palette...", progress: 0.1}

			palFilter := gifVfStr
			if palFilter != "" {
				palFilter += ","
			}
			palFilter += "palettegen"
			palArgs := []string{"-y"}
			palArgs = append(palArgs, trimArgs...)
			palArgs = append(palArgs, "-i", inputFile, "-vf", palFilter, paletteFile)

			if err := runFFmpeg(palArgs, progressChan, duration, "GIF Palette"); err != nil {
				return workDoneMsg{err: err}
			}

			progressChan <- progressMsg{line: "Encoding GIF...", progress: 0.5}

			filterComplex := fmt.Sprintf("[0:v]%s[x];[x][1:v]paletteuse", gifVfStr)
			if gifVfStr == "" {
				filterComplex = "[0:v]fifo[x];[x][1:v]paletteuse"
			}

			encArgs := []string{"-y"}
			encArgs = append(encArgs, trimArgs...)
			encArgs = append(encArgs,
				"-i", inputFile, "-i", paletteFile,
				"-lavfi", filterComplex,
				outputFile,
			)

			if err := runFFmpeg(encArgs, progressChan, duration, "GIF Encode"); err != nil {
				return workDoneMsg{err: err}
			}

			return finishWork(outputFile)
		}

		// video mode
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

		filterArgs := []string{}
		if vfString != "" {
			filterArgs = []string{"-vf", vfString}
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

			nullOut := "/dev/null"
			if runtime.GOOS == "windows" {
				nullOut = "NUL"
			}

			// pass 1
			p1 := []string{"-y"}
			p1 = append(p1, trimArgs...)
			p1 = append(p1, "-i", inputFile, "-c:v", codecCfg.FFmpegLib, "-b:v", fmt.Sprintf("%dk", videoKBit), "-pass", "1", "-passlogfile", passLog, "-an")
			p1 = append(p1, filterArgs...)
			p1 = append(p1, extraArgs...)
			p1 = append(p1, "-f", "null", nullOut)

			if err := runFFmpeg(p1, progressChan, duration, "Pass 1 (Analysis)"); err != nil {
				return workDoneMsg{err: err}
			}

			// pass 2
			p2 := []string{"-y"}
			p2 = append(p2, trimArgs...)
			p2 = append(p2, "-i", inputFile, "-c:v", codecCfg.FFmpegLib, "-b:v", fmt.Sprintf("%dk", videoKBit), "-pass", "2", "-passlogfile", passLog)
			p2 = append(p2, filterArgs...)
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
			extraArgs := []string{}
			if strings.Contains(codecCfg.FFmpegLib, "nvenc") {
				extraArgs = []string{"-preset", "p7", "-rc", "vbr", "-cq", "0"}
			} else if strings.Contains(codecCfg.FFmpegLib, "amf") {
				extraArgs = []string{"-quality", "quality"}
			} else if strings.Contains(codecCfg.FFmpegLib, "qsv") {
				extraArgs = []string{"-preset", "veryslow"}
			}

			cmdArgs := []string{"-y", "-hwaccel", "auto"}
			cmdArgs = append(cmdArgs, trimArgs...)
			cmdArgs = append(cmdArgs,
				"-i", inputFile,
				"-c:v", codecCfg.FFmpegLib,
				"-b:v", fmt.Sprintf("%dk", videoKBit),
				"-maxrate", fmt.Sprintf("%dk", videoKBit),
				"-bufsize", fmt.Sprintf("%dk", videoKBit*2),
			)
			cmdArgs = append(cmdArgs, filterArgs...)
			cmdArgs = append(cmdArgs, extraArgs...)
			cmdArgs = append(cmdArgs, audioArgs...)
			cmdArgs = append(cmdArgs, outputFile)

			if err := runFFmpeg(cmdArgs, progressChan, duration, "GPU Encoding"); err != nil {
				return workDoneMsg{err: err}
			}
		}

		return finishWork(outputFile)
	}
}

func finishWork(path string) workDoneMsg {
	fi, err := os.Stat(path)
	sizeStr := "Unknown"
	if err == nil {
		mb := float64(fi.Size()) / 1024 / 1024
		sizeStr = fmt.Sprintf("%.2f MB", mb)
	}
	return workDoneMsg{outputFile: path, finalSize: sizeStr, err: nil}
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

func printHelp() {
	fmt.Println(titleStyle.Render(" Teacrush "))
	fmt.Println("\nUsage:")
	fmt.Println("  teacrush [input_file] [flags]")
	fmt.Println("\nFlags:")
	fmt.Println("  -gif                Encode to GIF")
	fmt.Println("  -trim [start] [end] Trim video (e.g. -trim 00:01:00 00:02:00 or -trim 1s 5s)")
	fmt.Println("  -h, --help, ?       Show this help message")
}

func main() {
	isGif := false
	for _, arg := range os.Args {
		if arg == "-h" || arg == "--help" || arg == "?" {
			printHelp()
			os.Exit(0)
		}
		if arg == "-gif" {
			isGif = true
		}
	}

	p := tea.NewProgram(initialModel(isGif))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
