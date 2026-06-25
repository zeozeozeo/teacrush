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

const (
	appMarginY = 1
	appMarginX = 2
)

type cropDragMode int

const (
	cropDragNone cropDragMode = iota
	cropDragMove
	cropDragLeft
	cropDragRight
	cropDragTop
	cropDragBottom
	cropDragTopLeft
	cropDragTopRight
	cropDragBottomLeft
	cropDragBottomRight
)

var (
	appStyle = lipgloss.NewStyle().Margin(appMarginY, appMarginX)

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

	cmdBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1).
			Foreground(lipgloss.Color("245")).
			Width(78)

	previewBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)
)

type state int

const (
	stateInputFile state = iota
	stateInputSize
	stateInputRes
	stateFPS
	stateInputTrim
	stateInputCrop
	stateSelectHW
	stateSelectCodec
	stateSelectCRF
	stateSelectQuality
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
		{"AV1 (SVT-AV1, Balanced, Recommended)", "libsvtav1", ".webm"},
		{"AV1 (AOM, Reference/Slow)", "libaom-av1", ".webm"},
		{"AV1 (rav1e)", "librav1e", ".webm"},
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
	debugCmd string
}

type workDoneMsg struct {
	outputFile string
	finalSize  string
	err        error
}

type clipInfoMsg struct {
	duration float64
	width    int
	height   int
	err      error
}

type framePreviewMsg struct {
	time   float64
	width  int
	height int
	seq    int
	pixels []byte
	err    error
}

type previewOverlay struct {
	x            int
	y            int
	w            int
	h            int
	sourceWidth  int
	sourceHeight int
	dimOutside   bool
}

type cropHandle struct {
	x    int
	y    int
	mode cropDragMode
}

type outputMode int

const (
	modeVideo outputMode = iota
	modeGIF
	modeAPNG
	modeAVIF
)

type model struct {
	state     state
	textInput textinput.Model
	spinner   spinner.Model
	err       error

	outputMode outputMode
	verbose    bool
	customOut  string

	filePath      string
	originalSize  float64
	targetSizeMB  float64
	targetRes     string
	targetFPS     string // empty = real
	trimStart     string
	trimEnd       string
	trimStartSec  float64
	trimEndSec    float64
	trimHandle    int // 0 = start, 1 = end
	trimDragging  bool
	cropInput     string
	cropEnabled   bool
	cropX         int
	cropY         int
	cropW         int
	cropH         int
	cropDragMode  cropDragMode
	cropDragStart struct {
		mouseX int
		mouseY int
		x      int
		y      int
		w      int
		h      int
	}
	selectedHW    int
	selectedCodec int
	crfLevel      int // 0 to 10
	qualityLevel  int // 0 to 4

	progressChan chan progressMsg
	currentLog   string
	currentCmd   string
	percent      float64
	outputFile   string
	finalSize    string

	videoDuration   float64
	videoWidth      int
	videoHeight     int
	clipInfoLoading bool
	previewTime     float64
	previewWidth    int
	previewHeight   int
	previewSeq      int
	previewLoading  bool
	previewPixels   []byte
	framePreview    string
	previewError    string
	windowWidth     int
	windowHeight    int

	suggestions   []string
	suggestionIdx int
}

func initialModel(mode outputMode) model {
	ti := textinput.New()
	ti.CharLimit = 1000
	ti.Width = 60
	ti.Focus()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	m := model{
		state:        stateInputFile,
		spinner:      s,
		selectedHW:   0,
		crfLevel:     5, // medium/balanced quality
		qualityLevel: 2, // balanced speed
		outputMode:   mode,
	}

	args := os.Args[1:]
	skip := 0
	for i, arg := range args {
		if skip > 0 {
			skip--
			continue
		}
		if arg == "-gif" || arg == "-apng" || arg == "-avif" {
			continue
		}
		if arg == "-v" {
			m.verbose = true
			continue
		}
		if arg == "-o" {
			if i+1 < len(args) {
				m.customOut = args[i+1]
				skip = 1
				continue
			}
		}
		if arg == "-trim" {
			if i+2 < len(args) {
				m.trimStart = args[i+1]
				m.trimEnd = args[i+2]
				skip = 2
				continue
			}
		}
		if arg == "-crop" {
			if i+1 < len(args) {
				m.cropInput = args[i+1]
				skip = 1
				continue
			}
		}

		clean := cleanPath(arg)
		if fi, err := os.Stat(clean); err == nil {
			m.filePath = clean
			m.originalSize = float64(fi.Size()) / 1024 / 1024

			if m.outputMode == modeGIF || m.outputMode == modeAPNG {
				m.state = stateInputRes
				ti.Placeholder = "Enter=Original, 2=Half-size, or e.g. 1280x720"
			} else {
				m.state = stateInputSize
				ti.Placeholder = "e.g. 10 (for 10MB)"
			}
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
	case tea.WindowSizeMsg:
		oldPreviewWidth, oldPreviewHeight := m.previewDimensions()
		m.windowWidth = msg.Width
		m.windowHeight = msg.Height
		newPreviewWidth, newPreviewHeight := m.previewDimensions()
		if m.state == stateInputTrim && !m.clipInfoLoading && (oldPreviewWidth != newPreviewWidth || oldPreviewHeight != newPreviewHeight) {
			return m.loadCurrentPreview()
		}
		return m, nil

	case tea.MouseMsg:
		if updated, mouseCmd, handled := m.handleTrimMouse(msg); handled {
			return updated, mouseCmd
		}
		if updated, mouseCmd, handled := m.handleCropMouse(msg); handled {
			return updated, mouseCmd
		}

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
				if fi, err := os.Stat(path); err != nil {
					m.err = fmt.Errorf("file not found: %s", path)
				} else {
					m.filePath = path
					m.originalSize = float64(fi.Size()) / 1024 / 1024

					if m.outputMode == modeGIF || m.outputMode == modeAPNG {
						m.state = stateInputRes
						m.textInput.Reset()
						m.textInput.Placeholder = "Enter=Original, 2=Half-size, or e.g. 1280x720"
					} else {
						m.state = stateInputSize
						m.textInput.Reset()
						m.textInput.Placeholder = "e.g. 10 (for 10MB)"
					}
					m.err = nil
				}
			}

		case stateInputSize:
			if msg.Type == tea.KeyEnter {
				val := m.textInput.Value()
				if val == "" {
					m.targetSizeMB = 0 // will use CRF mode
					m.state = stateInputRes
					m.textInput.Reset()
					m.textInput.Placeholder = "Enter=Original, 2=Half-size, or e.g. 1280x720"
					m.err = nil
				} else {
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
			}

		case stateInputRes:
			if msg.Type == tea.KeyEnter {
				m.targetRes = m.textInput.Value()
				m.textInput.Reset()
				m.state = stateFPS
				m.textInput.Placeholder = "Enter=Original, or e.g. 30, 60"
				m.err = nil
			}

		case stateFPS:
			if msg.Type == tea.KeyEnter {
				m.targetFPS = strings.TrimSpace(m.textInput.Value())
				m.textInput.Reset()
				m.textInput.Blur()
				m.state = stateInputTrim
				m.clipInfoLoading = true
				m.previewLoading = false
				m.framePreview = ""
				m.previewError = ""
				m.err = nil
				return m, loadClipInfo(m.filePath)
			}

		case stateInputTrim:
			if m.clipInfoLoading {
				return m, nil
			}

			switch msg.String() {
			case "left", "h", "a":
				m = m.moveTrimHandle(-m.trimStep(false))
				return m.loadCurrentPreview()
			case "right", "l", "d":
				m = m.moveTrimHandle(m.trimStep(false))
				return m.loadCurrentPreview()
			case "shift+left", "ctrl+left", "pgup":
				m = m.moveTrimHandle(-m.trimStep(true))
				return m.loadCurrentPreview()
			case "shift+right", "ctrl+right", "pgdown":
				m = m.moveTrimHandle(m.trimStep(true))
				return m.loadCurrentPreview()
			case "tab":
				m.trimHandle = 1 - m.trimHandle
				return m.loadCurrentPreview()
			case "r":
				m = m.resetTrimSelection()
				return m.loadCurrentPreview()
			case "enter":
				m = m.commitTrimSelection()
				m.textInput.Reset()
				m.textInput.Blur()
				m = m.initializeCropSelection()
				m.state = stateInputCrop
				m.framePreview = m.renderPreviewWithCrop()
				m.err = nil
			}

		case stateInputCrop:
			switch msg.String() {
			case "left", "h", "a":
				m = m.moveCrop(-m.cropStepX(false), 0)
			case "right", "l", "d":
				m = m.moveCrop(m.cropStepX(false), 0)
			case "up", "k", "w":
				m = m.moveCrop(0, -m.cropStepY(false))
			case "down", "j", "s":
				m = m.moveCrop(0, m.cropStepY(false))
			case "shift+left", "ctrl+left", "pgup":
				m = m.resizeCrop(-m.cropStepX(true), 0)
			case "shift+right", "ctrl+right", "pgdown":
				m = m.resizeCrop(m.cropStepX(true), 0)
			case "shift+up", "ctrl+up":
				m = m.resizeCrop(0, -m.cropStepY(true))
			case "shift+down", "ctrl+down":
				m = m.resizeCrop(0, m.cropStepY(true))
			case "r":
				m = m.resetCropSelection()
			case "q":
				m = m.setSquareCrop()
			case "n":
				m.cropEnabled = false
				m = m.commitCropSelection()
			case "enter":
				m = m.commitCropSelection()
				return m.startAfterCrop()
			}
			m = m.commitCropSelection()
			m.framePreview = m.renderPreviewWithCrop()

		case stateSelectHW:
			switch msg.String() {
			case "up", "k", "w":
				if m.selectedHW > 0 {
					m.selectedHW--
				}
			case "down", "j", "s":
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
			if m.outputMode == modeAVIF {
				var av1Options []codecInfo
				for _, c := range options {
					if strings.Contains(c.FFmpegLib, "av1") {
						av1Options = append(av1Options, c)
					}
				}
				options = av1Options
			}

			switch msg.String() {
			case "up", "k", "w":
				if m.selectedCodec > 0 {
					m.selectedCodec--
				}
			case "down", "j", "s":
				if m.selectedCodec < len(options)-1 {
					m.selectedCodec++
				}
			case "enter":
				if len(options) == 0 {
					return m, nil
				}
				if m.targetSizeMB <= 0 {
					m.state = stateSelectCRF
				} else {
					m.state = stateSelectQuality
				}
			}

		case stateSelectCRF:
			switch msg.String() {
			case "left", "h", "a":
				if m.crfLevel > 0 {
					m.crfLevel--
				}
			case "right", "l", "d":
				if m.crfLevel < 10 {
					m.crfLevel++
				}
			case "enter":
				m.state = stateSelectQuality
			}

		case stateSelectQuality:
			switch msg.String() {
			case "left", "h", "a":
				if m.qualityLevel > 0 {
					m.qualityLevel--
				}
			case "right", "l", "d":
				if m.qualityLevel < 4 {
					m.qualityLevel++
				}
			case "enter":
				hw := hardwareOptions[m.selectedHW]
				options := encoderMap[hw]
				if m.outputMode == modeAVIF {
					var av1Options []codecInfo
					for _, c := range options {
						if strings.Contains(c.FFmpegLib, "av1") {
							av1Options = append(av1Options, c)
						}
					}
					options = av1Options
				}
				codecCfg := options[m.selectedCodec]

				m.state = stateProcessing
				m.progressChan = make(chan progressMsg)

				return m, tea.Batch(
					m.spinner.Tick,
					startEncoding(m.filePath, m.targetSizeMB, m.targetRes, m.targetFPS, m.trimStart, m.trimEnd, m.cropInput, m.customOut, hw, codecCfg, m.progressChan, m.outputMode, m.qualityLevel, m.crfLevel),
					waitForProgress(m.progressChan),
				)
			}
		}

	case progressMsg:
		m.currentLog = msg.line
		if msg.progress > 0 {
			m.percent = msg.progress
		}
		if msg.debugCmd != "" {
			m.currentCmd = msg.debugCmd
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

	case clipInfoMsg:
		m.clipInfoLoading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.videoWidth = msg.width
		m.videoHeight = msg.height
		m = m.initializeTrimSelection(msg.duration)
		return m.loadCurrentPreview()

	case framePreviewMsg:
		if math.Abs(msg.time-m.previewTime) > 0.01 || msg.width != m.previewWidth || msg.height != m.previewHeight {
			return m, nil
		}
		m.previewLoading = false
		if msg.err != nil {
			m.previewError = msg.err.Error()
			return m, nil
		}
		m.previewPixels = msg.pixels
		m.framePreview = renderANSIFrame(msg.pixels, msg.width, msg.height, nil)
		m.previewError = ""
		return m, nil

	case spinner.TickMsg:
		if m.state == stateProcessing {
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	}

	if m.state == stateInputFile || m.state == stateInputSize || m.state == stateInputRes || m.state == stateFPS || m.state == stateInputCrop {
		m.textInput, cmd = m.textInput.Update(msg)
	}

	return m, cmd
}

func (m model) View() string {
	var s strings.Builder

	title := " Teacrush "
	switch m.outputMode {
	case modeGIF:
		title += "(GIF Mode)"
	case modeAPNG:
		title += "(APNG Mode)"
	case modeAVIF:
		title += "(AVIF Mode)"
	}
	s.WriteString(titleStyle.Render(title))
	if m.trimStart != "" {
		s.WriteString(fmt.Sprintf(" [Trim: %s-%s]", m.trimStart, m.trimEnd))
	}
	if m.cropInput != "" {
		s.WriteString(fmt.Sprintf(" [Crop: %s]", m.cropInput))
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
		switch m.outputMode {
		case modeGIF:
			s.WriteString("\nMax MB (GIF), Empty=CRF:\n\n")
		case modeAPNG:
			s.WriteString("\nMax MB (APNG), Empty=CRF:\n\n")
		case modeAVIF:
			s.WriteString("\nMax MB (AVIF), Empty=CRF:\n\n")
		default:
			s.WriteString("\nMax MB (Audio+Video), Empty=CRF:\n\n")
		}
		s.WriteString(m.textInput.View())

	case stateInputRes:
		s.WriteString(stepStyle.Render("3. Target Resolution"))
		s.WriteString("\nLeave empty for original.")
		s.WriteString("\nType '2' for half size (1/2).")
		s.WriteString("\nType '1280x720' for fixed size.\n\n")
		s.WriteString(m.textInput.View())

	case stateFPS:
		stepTitle := "4. Target Framerate (FPS)"
		s.WriteString(stepStyle.Render(stepTitle))
		s.WriteString("\nLeave empty for original FPS.")
		s.WriteString("\nEnter a number (e.g. 30, 60) to set FPS.\n\n")
		s.WriteString(m.textInput.View())

	case stateInputTrim:
		s.WriteString(stepStyle.Render("5. Trim Clip"))
		if m.clipInfoLoading {
			s.WriteString("\nReading clip duration...")
			break
		}

		active := "start"
		if m.trimHandle == 1 {
			active = "end"
		}
		s.WriteString(fmt.Sprintf("\n%s: %s  %s: %s  duration: %s", labelForHandle("start", m.trimHandle == 0), formatDuration(m.trimStartSec), labelForHandle("end", m.trimHandle == 1), formatDuration(m.trimEndSec), formatDuration(math.Max(0, m.trimEndSec-m.trimStartSec))))
		s.WriteString("\n")
		s.WriteString(m.renderTrimSlider(m.trimSliderWidth()))
		s.WriteString(fmt.Sprintf("\n\nEditing %s handle. Drag slider or use Left/Right, PgUp/PgDn, Tab, R, Enter.\n\n", active))

		if m.previewLoading && m.framePreview == "" {
			s.WriteString(itemStyle.Render("Loading frame preview..."))
		} else if m.previewError != "" {
			s.WriteString(itemStyle.Render("Preview unavailable: " + m.previewError))
		} else if m.framePreview != "" {
			caption := fmt.Sprintf("Frame at %s", formatDuration(m.previewTime))
			if m.previewLoading {
				caption += " (updating)"
			}
			s.WriteString(previewBoxStyle.Render(caption + "\n" + m.framePreview))
		}

	case stateInputCrop:
		s.WriteString(stepStyle.Render("6. Crop Frame"))
		cropLabel := "full frame"
		if m.cropInput != "" {
			cropLabel = m.cropInput
		}
		s.WriteString(fmt.Sprintf("\nCrop: %s", selectedItemStyle.Render(cropLabel)))
		s.WriteString("\nDrag inside the box to move it, or drag an edge/corner to resize.")
		s.WriteString("\nArrow keys move. Shift/Ctrl+Arrows resize. Q square, R reset, N none, Enter continues.\n\n")

		if m.previewLoading && m.framePreview == "" {
			s.WriteString(itemStyle.Render("Loading frame preview..."))
		} else if m.previewError != "" {
			s.WriteString(itemStyle.Render("Preview unavailable: " + m.previewError))
		} else if m.framePreview != "" {
			s.WriteString(previewBoxStyle.Render("Crop preview\n" + m.framePreview))
		}

	case stateSelectHW:
		s.WriteString(stepStyle.Render("7. Select Hardware"))
		if m.targetSizeMB > 0 {
			s.WriteString(fmt.Sprintf("\nTarget: %.2f MB\n\n", m.targetSizeMB))
		} else {
			s.WriteString("\nTarget: CRF\n\n")
		}
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
		s.WriteString(stepStyle.Render("8. Select Codec"))
		if m.outputMode == modeAVIF {
			s.WriteString(" (AV1 only)")
		}
		hw := hardwareOptions[m.selectedHW]
		s.WriteString(fmt.Sprintf("\nHardware: %s\n\n", hw))

		options := encoderMap[hw]
		if m.outputMode == modeAVIF {
			var av1Options []codecInfo
			for _, c := range options {
				if strings.Contains(c.FFmpegLib, "av1") {
					av1Options = append(av1Options, c)
				}
			}
			options = av1Options
		}

		for i, c := range options {
			cursor := "  "
			style := itemStyle
			if m.selectedCodec == i {
				cursor = "> "
				style = selectedItemStyle
			}
			s.WriteString(style.Render(cursor+c.Name) + "\n")
		}

	case stateSelectCRF:
		s.WriteString(stepStyle.Render("9. Quality (CRF)"))
		s.WriteString("\nAdjust the Constant Rate Factor (CRF).")
		s.WriteString("\n\n")

		sliderWidth := 20
		pos := m.crfLevel * (sliderWidth / 10)
		line := ""
		for i := 0; i <= sliderWidth; i++ {
			if i == pos {
				line += "○"
			} else {
				line += "━"
			}
		}

		estimatedMB := m.originalSize * (0.6 * math.Pow(1.2, float64(5-m.crfLevel)))
		s.WriteString(fmt.Sprintf("  High Quality  [ %s ]  Smaller File\n", line))
		s.WriteString(fmt.Sprintf("  Estimated Size: %s\n", selectedItemStyle.Render(fmt.Sprintf("~%.1f MB", estimatedMB))))
		s.WriteString("\nPress Enter to continue.")

	case stateSelectQuality:
		stepNum := "9"
		if m.targetSizeMB <= 0 {
			stepNum = "10"
		}
		s.WriteString(stepStyle.Render(stepNum + ". Select Encoding Speed"))
		s.WriteString("\nUse Left/Right to adjust.")
		s.WriteString("\n\n")

		sliderWidth := 20
		pos := m.qualityLevel * (sliderWidth / 4)
		line := ""
		for i := 0; i <= sliderWidth; i++ {
			if i == pos {
				line += "○"
			} else {
				line += "━"
			}
		}

		labels := []string{"Fastest", "Faster", "Balanced (default)", "Better", "Best"}
		currentLabel := labels[m.qualityLevel]

		s.WriteString(fmt.Sprintf("  Fast  [ %s ]  Slow\n", line))
		s.WriteString("  Mode: " + selectedItemStyle.Render(currentLabel))
		s.WriteString("\n\nPress Enter to start.")

	case stateProcessing:
		mode := "Compressing"
		switch m.outputMode {
		case modeGIF:
			mode = "Creating GIF"
		case modeAPNG:
			mode = "Creating APNG"
		case modeAVIF:
			mode = "Creating AVIF"
		}
		s.WriteString(stepStyle.Render(mode + "..."))
		s.WriteString("\n\n")

		width := 40
		filled := int(math.Max(0, math.Min(float64(width), m.percent*float64(width))))
		bar := progressFullStyle.Render(strings.Repeat("█", filled)) +
			progressEmptyStyle.Render(strings.Repeat("░", width-filled))

		s.WriteString(fmt.Sprintf("%s %s  %.0f%%\n\n", m.spinner.View(), bar, m.percent*100))
		s.WriteString(lipgloss.NewStyle().Faint(true).Render("Status: " + m.currentLog))

		if m.verbose && m.currentCmd != "" {
			s.WriteString("\n\n")
			s.WriteString(cmdBoxStyle.Render(lipgloss.NewStyle().Width(76).Render(m.currentCmd)))
		}

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

func loadClipInfo(path string) tea.Cmd {
	return func() tea.Msg {
		info, err := probeFile(path)
		if err != nil {
			return clipInfoMsg{err: err}
		}
		duration, err := strconv.ParseFloat(info.Format.Duration, 64)
		if err != nil || duration <= 0 {
			return clipInfoMsg{err: fmt.Errorf("could not read clip duration")}
		}

		width, height := info.videoDimensions()
		return clipInfoMsg{duration: duration, width: width, height: height}
	}
}

func loadFramePreview(path string, at float64, width int, height int, seq int) tea.Cmd {
	return func() tea.Msg {
		pixels, err := renderFramePreview(path, at, width, height)
		return framePreviewMsg{time: at, width: width, height: height, seq: seq, pixels: pixels, err: err}
	}
}

func (m model) initializeTrimSelection(duration float64) model {
	m.videoDuration = duration
	m.trimHandle = 0
	m.trimStartSec = 0
	m.trimEndSec = duration

	if m.trimStart != "" || m.trimEnd != "" {
		start, errStart := parseDurationStrict(m.trimStart)
		end, errEnd := parseDurationStrict(m.trimEnd)
		if errStart == nil && errEnd == nil && end > start {
			m.trimStartSec = clampFloat(start, 0, duration)
			m.trimEndSec = clampFloat(end, m.trimStartSec, duration)
		}
	}

	m = m.commitTrimSelection()
	return m
}

func (m model) resetTrimSelection() model {
	m.trimStartSec = 0
	m.trimEndSec = m.videoDuration
	m.trimHandle = 0
	m = m.commitTrimSelection()
	return m
}

func (m model) moveTrimHandle(delta float64) model {
	if m.videoDuration <= 0 {
		return m
	}

	minGap := math.Min(0.1, m.videoDuration)
	if m.trimHandle == 0 {
		m.trimStartSec = clampFloat(m.trimStartSec+delta, 0, math.Max(0, m.trimEndSec-minGap))
	} else {
		m.trimEndSec = clampFloat(m.trimEndSec+delta, math.Min(m.videoDuration, m.trimStartSec+minGap), m.videoDuration)
	}

	m = m.commitTrimSelection()
	return m
}

func (m model) trimStep(big bool) float64 {
	if m.videoDuration <= 0 {
		return 1
	}
	step := math.Max(0.25, m.videoDuration/200)
	if big {
		step = math.Max(1, m.videoDuration/40)
	}
	return step
}

func (m model) commitTrimSelection() model {
	if m.videoDuration <= 0 || (m.trimStartSec <= 0 && math.Abs(m.trimEndSec-m.videoDuration) < 0.01) {
		m.trimStart = ""
		m.trimEnd = ""
		return m
	}
	m.trimStart = formatDuration(m.trimStartSec)
	m.trimEnd = formatDuration(m.trimEndSec)
	return m
}

func (m model) activeTrimTime() float64 {
	if m.trimHandle == 1 {
		return m.trimEndSec
	}
	return m.trimStartSec
}

func (m model) loadCurrentPreview() (model, tea.Cmd) {
	at := m.activeTrimTime()
	if m.videoDuration > 0 {
		at = clampFloat(at, 0, math.Max(0, m.videoDuration-0.05))
	}
	width, height := m.previewDimensions()
	m.previewSeq++
	m.previewTime = at
	m.previewWidth = width
	m.previewHeight = height
	m.previewLoading = true
	m.previewError = ""
	return m, loadFramePreview(m.filePath, at, width, height, m.previewSeq)
}

func (m model) handleTrimMouse(msg tea.MouseMsg) (model, tea.Cmd, bool) {
	if m.state != stateInputTrim || m.clipInfoLoading || m.videoDuration <= 0 {
		return m, nil, false
	}

	event := tea.MouseEvent(msg)
	switch event.Action {
	case tea.MouseActionPress:
		if event.Button != tea.MouseButtonLeft || !m.mouseOnTrimSlider(event.X, event.Y) {
			return m, nil, false
		}
		m.trimDragging = true
		m, changed := m.setTrimHandleFromX(event.X, true)
		if changed {
			updated, cmd := m.loadCurrentPreview()
			return updated, cmd, true
		}
		return m, nil, true

	case tea.MouseActionMotion:
		if !m.trimDragging {
			if event.Button != tea.MouseButtonLeft || !m.mouseOnTrimSlider(event.X, event.Y) {
				return m, nil, false
			}
			m.trimDragging = true
		}
		m, changed := m.setTrimHandleFromX(event.X, false)
		if changed {
			updated, cmd := m.loadCurrentPreview()
			return updated, cmd, true
		}
		return m, nil, true

	case tea.MouseActionRelease:
		if !m.trimDragging {
			return m, nil, false
		}
		m.trimDragging = false
		return m, nil, true
	}

	return m, nil, false
}

func (m model) handleCropMouse(msg tea.MouseMsg) (model, tea.Cmd, bool) {
	if m.state != stateInputCrop || m.previewWidth <= 0 || m.previewHeight <= 0 || len(m.previewPixels) == 0 {
		return m, nil, false
	}

	event := tea.MouseEvent(msg)
	switch event.Action {
	case tea.MouseActionPress:
		if event.Button != tea.MouseButtonLeft || !m.mouseOnPreview(event.X, event.Y) {
			return m, nil, false
		}
		m.cropDragMode = m.cropDragModeAt(event.X, event.Y)
		if m.cropDragMode == cropDragNone {
			return m, nil, false
		}
		m.cropEnabled = true
		m.cropDragStart.mouseX = event.X
		m.cropDragStart.mouseY = event.Y
		m.cropDragStart.x = m.cropX
		m.cropDragStart.y = m.cropY
		m.cropDragStart.w = m.cropW
		m.cropDragStart.h = m.cropH
		m.framePreview = m.renderPreviewWithCrop()
		return m, nil, true

	case tea.MouseActionMotion:
		if m.cropDragMode == cropDragNone {
			if event.Button != tea.MouseButtonLeft || !m.mouseOnPreview(event.X, event.Y) {
				return m, nil, false
			}
			m.cropDragMode = m.cropDragModeAt(event.X, event.Y)
			m.cropEnabled = true
			m.cropDragStart.mouseX = event.X
			m.cropDragStart.mouseY = event.Y
			m.cropDragStart.x = m.cropX
			m.cropDragStart.y = m.cropY
			m.cropDragStart.w = m.cropW
			m.cropDragStart.h = m.cropH
		}
		m = m.dragCropTo(event.X, event.Y)
		m = m.commitCropSelection()
		m.framePreview = m.renderPreviewWithCrop()
		return m, nil, true

	case tea.MouseActionRelease:
		if m.cropDragMode == cropDragNone {
			return m, nil, false
		}
		m.cropDragMode = cropDragNone
		m = m.commitCropSelection()
		m.framePreview = m.renderPreviewWithCrop()
		return m, nil, true
	}

	return m, nil, false
}

func (m model) setTrimHandleFromX(x int, chooseNearest bool) (model, bool) {
	if m.videoDuration <= 0 {
		return m, false
	}

	sliderX, _ := m.trimSliderOrigin()
	width := m.trimSliderWidth()
	pos := clampInt(x-sliderX, 0, width-1)
	seconds := (float64(pos) / float64(width-1)) * m.videoDuration

	if chooseNearest {
		if math.Abs(seconds-m.trimEndSec) < math.Abs(seconds-m.trimStartSec) {
			m.trimHandle = 1
		} else {
			m.trimHandle = 0
		}
	}

	oldActive := m.activeTrimTime()
	minGap := math.Min(0.1, m.videoDuration)
	if m.trimHandle == 0 {
		m.trimStartSec = clampFloat(seconds, 0, math.Max(0, m.trimEndSec-minGap))
	} else {
		m.trimEndSec = clampFloat(seconds, math.Min(m.videoDuration, m.trimStartSec+minGap), m.videoDuration)
	}
	m = m.commitTrimSelection()

	return m, math.Abs(m.activeTrimTime()-oldActive) >= 0.01
}

func (m model) mouseOnTrimSlider(x int, y int) bool {
	sliderX, sliderY := m.trimSliderOrigin()
	width := m.trimSliderWidth()
	return y >= sliderY-1 && y <= sliderY+1 && x >= sliderX && x < sliderX+width
}

func (m model) trimSliderOrigin() (int, int) {
	errorRows := 0
	if m.err != nil {
		errorRows = 2
	}
	return appMarginX, appMarginY + errorRows + 4
}

func (m model) trimSliderWidth() int {
	if m.windowWidth <= 0 {
		return 58
	}
	return clampInt(m.windowWidth-(appMarginX*2)-4, 30, 100)
}

func (m model) previewDimensions() (int, int) {
	width := 72
	if m.windowWidth > 0 {
		width = m.windowWidth - (appMarginX * 2) - 6
	}
	width = clampInt(width, 48, 120)

	height := int(math.Round(float64(width) * 9 / 16))
	height = clampInt(height, 24, 56)
	if m.windowHeight > 0 {
		availableRows := m.windowHeight - (appMarginY * 2) - 12
		maxHeight := math.Max(18, float64(availableRows*2))
		height = int(math.Min(float64(height), maxHeight))
	}
	if height%2 != 0 {
		height--
	}
	if height < 18 {
		height = 18
	}

	return width, height
}

func (m model) previewOrigin() (int, int) {
	errorRows := 0
	if m.err != nil {
		errorRows = 2
	}
	switch m.state {
	case stateInputCrop:
		return appMarginX + 2, appMarginY + errorRows + 9
	default:
		return appMarginX + 2, appMarginY + errorRows + 10
	}
}

func (m model) mouseOnPreview(x int, y int) bool {
	previewX, previewY := m.previewOrigin()
	rows := (m.previewHeight + 1) / 2
	return x >= previewX && x < previewX+m.previewWidth && y >= previewY && y < previewY+rows
}

func (m model) cropDragModeAt(mouseX int, mouseY int) cropDragMode {
	cellX, cellY := m.previewMouseToCell(mouseX, mouseY)
	overlay := m.cropPreviewOverlay()

	if mode, ok := cropHandleAt(cellX, cellY, overlay); ok {
		return mode
	}
	if cropPointInside(cellX, cellY, overlay) {
		return cropDragMove
	}
	return cropDragNone
}

func (m model) previewMouseToCell(mouseX int, mouseY int) (int, int) {
	previewX, previewY := m.previewOrigin()
	x := clampInt(mouseX-previewX, 0, m.previewWidth-1)
	y := clampInt((mouseY-previewY)*2, 0, m.previewHeight-1)
	return x, y
}

func (m model) initializeCropSelection() model {
	if m.videoWidth <= 0 || m.videoHeight <= 0 {
		m.cropEnabled = false
		m.cropInput = ""
		return m
	}

	if crop, ok := parseCropInput(m.cropInput, m.videoWidth, m.videoHeight); ok {
		m.cropEnabled = true
		m.cropX = crop.x
		m.cropY = crop.y
		m.cropW = crop.w
		m.cropH = crop.h
	} else {
		m = m.resetCropSelection()
	}
	return m.commitCropSelection()
}

func (m model) resetCropSelection() model {
	m.cropEnabled = true
	m.cropX = 0
	m.cropY = 0
	m.cropW = m.videoWidth
	m.cropH = m.videoHeight
	return m.commitCropSelection()
}

func (m model) setSquareCrop() model {
	if m.videoWidth <= 0 || m.videoHeight <= 0 {
		return m
	}
	side := minInt(m.videoWidth, m.videoHeight)
	m.cropEnabled = true
	m.cropW = side
	m.cropH = side
	m.cropX = (m.videoWidth - side) / 2
	m.cropY = (m.videoHeight - side) / 2
	return m.commitCropSelection()
}

func (m model) moveCrop(dx int, dy int) model {
	if !m.cropEnabled {
		m.cropEnabled = true
	}
	m.cropX = clampInt(m.cropX+dx, 0, maxInt(0, m.videoWidth-m.cropW))
	m.cropY = clampInt(m.cropY+dy, 0, maxInt(0, m.videoHeight-m.cropH))
	return m.commitCropSelection()
}

func (m model) resizeCrop(dw int, dh int) model {
	if !m.cropEnabled {
		m.cropEnabled = true
	}
	m.cropW = clampInt(m.cropW+dw, 2, maxInt(2, m.videoWidth-m.cropX))
	m.cropH = clampInt(m.cropH+dh, 2, maxInt(2, m.videoHeight-m.cropY))
	return m.commitCropSelection()
}

func (m model) dragCropTo(mouseX int, mouseY int) model {
	cellX, cellY := m.previewMouseToCell(mouseX, mouseY)
	startCellX, startCellY := m.previewMouseToCell(m.cropDragStart.mouseX, m.cropDragStart.mouseY)
	dx := previewDeltaToSource(cellX-startCellX, m.previewWidth, m.videoWidth)
	dy := previewDeltaToSource(cellY-startCellY, m.previewHeight, m.videoHeight)
	x := m.cropDragStart.x
	y := m.cropDragStart.y
	w := m.cropDragStart.w
	h := m.cropDragStart.h

	switch m.cropDragMode {
	case cropDragMove:
		x = clampInt(x+dx, 0, maxInt(0, m.videoWidth-w))
		y = clampInt(y+dy, 0, maxInt(0, m.videoHeight-h))
	case cropDragLeft, cropDragTopLeft, cropDragBottomLeft:
		newX := clampInt(x+dx, 0, x+w-2)
		w += x - newX
		x = newX
	}
	switch m.cropDragMode {
	case cropDragRight, cropDragTopRight, cropDragBottomRight:
		w = clampInt(w+dx, 2, m.videoWidth-x)
	}
	switch m.cropDragMode {
	case cropDragTop, cropDragTopLeft, cropDragTopRight:
		newY := clampInt(y+dy, 0, y+h-2)
		h += y - newY
		y = newY
	}
	switch m.cropDragMode {
	case cropDragBottom, cropDragBottomLeft, cropDragBottomRight:
		h = clampInt(h+dy, 2, m.videoHeight-y)
	}

	m.cropEnabled = true
	m.cropX = clampInt(x, 0, maxInt(0, m.videoWidth-2))
	m.cropY = clampInt(y, 0, maxInt(0, m.videoHeight-2))
	m.cropW = clampInt(w, 2, maxInt(2, m.videoWidth-m.cropX))
	m.cropH = clampInt(h, 2, maxInt(2, m.videoHeight-m.cropY))
	return m
}

func (m model) commitCropSelection() model {
	if !m.cropEnabled || m.videoWidth <= 0 || m.videoHeight <= 0 {
		m.cropInput = ""
		return m
	}
	m.cropX = clampInt(m.cropX, 0, maxInt(0, m.videoWidth-2))
	m.cropY = clampInt(m.cropY, 0, maxInt(0, m.videoHeight-2))
	m.cropW = evenInt(clampInt(m.cropW, 2, maxInt(2, m.videoWidth-m.cropX)))
	m.cropH = evenInt(clampInt(m.cropH, 2, maxInt(2, m.videoHeight-m.cropY)))
	if m.cropX == 0 && m.cropY == 0 && m.cropW >= evenInt(m.videoWidth) && m.cropH >= evenInt(m.videoHeight) {
		m.cropInput = ""
		return m
	}
	m.cropInput = fmt.Sprintf("crop=%d:%d:%d:%d", m.cropW, m.cropH, m.cropX, m.cropY)
	return m
}

func (m model) renderPreviewWithCrop() string {
	if len(m.previewPixels) == 0 || m.previewWidth <= 0 || m.previewHeight <= 0 {
		return m.framePreview
	}
	overlay := m.cropPreviewOverlay()
	return renderANSIFrame(m.previewPixels, m.previewWidth, m.previewHeight, &overlay)
}

func (m model) cropPreviewOverlay() previewOverlay {
	if !m.cropEnabled || m.videoWidth <= 0 || m.videoHeight <= 0 {
		return previewOverlay{x: 0, y: 0, w: m.previewWidth, h: m.previewHeight, dimOutside: false}
	}
	x := int(math.Round(float64(m.cropX) / float64(m.videoWidth) * float64(m.previewWidth)))
	y := int(math.Round(float64(m.cropY) / float64(m.videoHeight) * float64(m.previewHeight)))
	w := int(math.Round(float64(m.cropW) / float64(m.videoWidth) * float64(m.previewWidth)))
	h := int(math.Round(float64(m.cropH) / float64(m.videoHeight) * float64(m.previewHeight)))
	x = clampInt(x, 0, maxInt(0, m.previewWidth-2))
	y = clampInt(y, 0, maxInt(0, m.previewHeight-2))
	w = clampInt(w, 2, m.previewWidth-x)
	h = clampInt(h, 2, m.previewHeight-y)
	return previewOverlay{x: x, y: y, w: w, h: h, sourceWidth: m.videoWidth, sourceHeight: m.videoHeight, dimOutside: true}
}

func (m model) cropStepX(big bool) int {
	step := m.cropStepForAxis(false, m.previewWidth, m.videoWidth, 1, 6)
	if big {
		return evenInt(step * 2)
	}
	return step
}

func (m model) cropStepY(big bool) int {
	step := m.cropStepForAxis(false, m.previewHeight, m.videoHeight, 1, 3)
	if big {
		return evenInt(step * 2)
	}
	return step
}

func (m model) cropStepForAxis(big bool, previewSize int, sourceSize int, cells int, divisor int) int {
	if previewSize > 0 && sourceSize > 0 {
		step := previewDeltaToSource(cells, previewSize, sourceSize)
		step = maxInt(2, step/divisor)
		if big {
			step *= 5
		}
		return evenInt(maxInt(2, step))
	}

	base := maxInt(2, minInt(m.videoWidth, m.videoHeight)/100)
	if big {
		base = maxInt(12, minInt(m.videoWidth, m.videoHeight)/25)
	}
	return evenInt(base)
}

func (m model) startAfterCrop() (tea.Model, tea.Cmd) {
	if m.outputMode == modeGIF || m.outputMode == modeAPNG {
		m.state = stateProcessing
		m.progressChan = make(chan progressMsg)
		var codecCfg codecInfo
		switch m.outputMode {
		case modeGIF:
			codecCfg = codecInfo{Name: "GIF", Ext: ".gif"}
		case modeAPNG:
			codecCfg = codecInfo{Name: "APNG", Ext: ".png"}
		}

		return m, tea.Batch(
			m.spinner.Tick,
			startEncoding(m.filePath, m.targetSizeMB, m.targetRes, m.targetFPS, m.trimStart, m.trimEnd, m.cropInput, m.customOut, hwCPU, codecCfg, m.progressChan, m.outputMode, m.qualityLevel, m.crfLevel),
			waitForProgress(m.progressChan),
		)
	}
	m.state = stateSelectHW
	return m, nil
}

func (m model) renderTrimSlider(width int) string {
	if width < 12 {
		width = 12
	}
	if m.videoDuration <= 0 {
		return progressEmptyStyle.Render(strings.Repeat("-", width))
	}

	startPos := int(math.Round((m.trimStartSec / m.videoDuration) * float64(width-1)))
	endPos := int(math.Round((m.trimEndSec / m.videoDuration) * float64(width-1)))
	startPos = clampInt(startPos, 0, width-1)
	endPos = clampInt(endPos, startPos, width-1)

	var b strings.Builder
	for i := 0; i < width; i++ {
		ch := "─"
		style := progressEmptyStyle
		if i >= startPos && i <= endPos {
			style = progressFullStyle
		}
		if i == startPos {
			ch = "◀"
			if m.trimHandle == 0 {
				style = selectedItemStyle
			}
		}
		if i == endPos {
			ch = "▶"
			if m.trimHandle == 1 {
				style = selectedItemStyle
			}
		}
		b.WriteString(style.Render(ch))
	}
	return b.String()
}

func renderFramePreview(path string, at float64, width int, height int) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid preview size")
	}

	args := []string{
		"-v", "error",
		"-ss", formatDuration(at),
		"-i", path,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=w=%d:h=%d:force_original_aspect_ratio=decrease:flags=lanczos,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", width, height, width, height),
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"-",
	}

	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	needed := width * height * 3
	if len(out) < needed {
		return nil, fmt.Errorf("ffmpeg returned an incomplete preview frame")
	}
	return out[:needed], nil
}

func renderANSIFrame(pixels []byte, width int, height int, overlay *previewOverlay) string {
	var b strings.Builder
	for y := 0; y < height; y += 2 {
		for x := 0; x < width; x++ {
			top := (y*width + x) * 3
			bottomY := y + 1
			bottom := top
			if bottomY < height {
				bottom = (bottomY*width + x) * 3
			}
			topR, topG, topB := pixels[top], pixels[top+1], pixels[top+2]
			bottomR, bottomG, bottomB := pixels[bottom], pixels[bottom+1], pixels[bottom+2]
			if overlay != nil {
				topR, topG, topB = applyOverlayColor(topR, topG, topB, x, y, *overlay)
				bottomR, bottomG, bottomB = applyOverlayColor(bottomR, bottomG, bottomB, x, bottomY, *overlay)
			}
			b.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", topR, topG, topB, bottomR, bottomG, bottomB))
		}
		b.WriteString("\x1b[0m")
		if y+2 < height {
			b.WriteByte('\n')
		}
	}

	return b.String()
}

func formatDuration(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	hours := int(sec) / 3600
	minutes := (int(sec) % 3600) / 60
	seconds := math.Mod(sec, 60)
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%05.2f", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%05.2f", minutes, seconds)
}

func clampFloat(value, min, max float64) float64 {
	if max < min {
		return min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func clampInt(value, min, max int) int {
	if max < min {
		return min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func evenInt(value int) int {
	if value < 2 {
		return 2
	}
	if value%2 != 0 {
		value--
	}
	return maxInt(2, value)
}

func previewDeltaToSource(delta int, previewSize int, sourceSize int) int {
	if previewSize <= 0 || sourceSize <= 0 || delta == 0 {
		return 0
	}
	return int(math.Round(float64(delta) / float64(previewSize) * float64(sourceSize)))
}

func labelForHandle(label string, active bool) string {
	if active {
		return selectedItemStyle.Render(label)
	}
	return itemStyle.Render(label)
}

func applyOverlayColor(r, g, b byte, x int, y int, overlay previewOverlay) (byte, byte, byte) {
	inside := x >= overlay.x && x < overlay.x+overlay.w && y >= overlay.y && y < overlay.y+overlay.h
	border := inside && (x == overlay.x || x == overlay.x+overlay.w-1 || y == overlay.y || y == overlay.y+overlay.h-1)

	if cropPointOnHandle(x, y, overlay) {
		return 255, 255, 255
	}
	if border {
		return 88, 101, 242
	}
	if overlay.dimOutside && !inside {
		return byte(float64(r) * 0.35), byte(float64(g) * 0.35), byte(float64(b) * 0.35)
	}
	return r, g, b
}

func cropPointInside(x int, y int, overlay previewOverlay) bool {
	return x >= overlay.x && x < overlay.x+overlay.w && y >= overlay.y && y < overlay.y+overlay.h
}

func cropPointOnHandle(x int, y int, overlay previewOverlay) bool {
	for _, handle := range cropHandles(overlay) {
		if x == handle.x && y == handle.y {
			return true
		}
	}
	return false
}

func cropHandleAt(x int, y int, overlay previewOverlay) (cropDragMode, bool) {
	for _, handle := range cropHandles(overlay) {
		if absInt(x-handle.x) <= 1 && absInt(y-handle.y) <= 1 {
			return handle.mode, true
		}
	}
	return cropDragNone, false
}

func cropHandles(overlay previewOverlay) []cropHandle {
	left := overlay.x
	right := overlay.x + overlay.w - 1
	top := overlay.y
	bottom := overlay.y + overlay.h - 1
	midX := overlay.x + overlay.w/2
	midY := overlay.y + overlay.h/2
	return []cropHandle{
		{x: left, y: top, mode: cropDragTopLeft},
		{x: midX, y: top, mode: cropDragTop},
		{x: right, y: top, mode: cropDragTopRight},
		{x: left, y: midY, mode: cropDragLeft},
		{x: right, y: midY, mode: cropDragRight},
		{x: left, y: bottom, mode: cropDragBottomLeft},
		{x: midX, y: bottom, mode: cropDragBottom},
		{x: right, y: bottom, mode: cropDragBottomRight},
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
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
	sec, _ := parseDurationStrict(s)
	return sec
}

func parseDurationStrict(s string) (float64, error) {
	s = strings.TrimSpace(strings.TrimSuffix(s, "s"))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	parts := strings.Split(s, ":")
	sec := 0.0
	mul := 1.0
	for i := len(parts) - 1; i >= 0; i-- {
		v, err := strconv.ParseFloat(parts[i], 64)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("invalid duration: %s", s)
		}
		sec += v * mul
		mul *= 60
	}
	return sec, nil
}

func parseTrimInput(input string) (string, string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", nil
	}

	fields := strings.Fields(input)
	if len(fields) != 2 && strings.Contains(input, "-") {
		parts := strings.SplitN(input, "-", 2)
		fields = []string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])}
	}
	if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
		return "", "", fmt.Errorf("trim must be empty or two times, e.g. 00:01 00:05")
	}

	start, err := parseDurationStrict(fields[0])
	if err != nil {
		return "", "", err
	}
	end, err := parseDurationStrict(fields[1])
	if err != nil {
		return "", "", err
	}
	if end <= start {
		return "", "", fmt.Errorf("trim end must be after trim start")
	}

	return fields[0], fields[1], nil
}

func buildCropFilter(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" || input == "1" {
		return "", nil
	}

	lower := strings.ToLower(input)
	if lower == "none" || lower == "no" {
		return "", nil
	}
	if lower == "square" || lower == "1:1" {
		return "crop=min(iw\\,ih):min(iw\\,ih):(iw-min(iw\\,ih))/2:(ih-min(iw\\,ih))/2", nil
	}
	if strings.HasPrefix(lower, "crop=") {
		return input, nil
	}

	if strings.Contains(input, "+") {
		parts := strings.Split(input, "+")
		if len(parts) != 3 {
			return "", fmt.Errorf("crop must be empty, square, WxH, WxH+X+Y, or crop=W:H:X:Y")
		}
		wh := splitDimension(parts[0])
		if len(wh) != 2 || wh[0] == "" || wh[1] == "" || parts[1] == "" || parts[2] == "" {
			return "", fmt.Errorf("crop must be empty, square, WxH, WxH+X+Y, or crop=W:H:X:Y")
		}
		return fmt.Sprintf("crop=%s:%s:%s:%s", wh[0], wh[1], parts[1], parts[2]), nil
	}

	if strings.ContainsAny(input, "xX") {
		wh := splitDimension(input)
		if len(wh) != 2 || wh[0] == "" || wh[1] == "" {
			return "", fmt.Errorf("crop must be empty, square, WxH, WxH+X+Y, or crop=W:H:X:Y")
		}
		return fmt.Sprintf("crop=%s:%s:(iw-%s)/2:(ih-%s)/2", wh[0], wh[1], wh[0], wh[1]), nil
	}

	if strings.Contains(input, ":") {
		return "crop=" + input, nil
	}

	return "", fmt.Errorf("crop must be empty, square, WxH, WxH+X+Y, or crop=W:H:X:Y")
}

type cropRect struct {
	x int
	y int
	w int
	h int
}

func parseCropInput(input string, videoWidth int, videoHeight int) (cropRect, bool) {
	filter, err := buildCropFilter(input)
	if err != nil || filter == "" || strings.Contains(filter, "min(") {
		return cropRect{}, false
	}
	filter = strings.TrimPrefix(filter, "crop=")
	parts := strings.Split(filter, ":")
	if len(parts) != 4 {
		return cropRect{}, false
	}
	values := make([]int, 4)
	for i, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return cropRect{}, false
		}
		values[i] = value
	}
	if values[0] <= 0 || values[1] <= 0 || values[2] < 0 || values[3] < 0 || values[2] >= videoWidth || values[3] >= videoHeight {
		return cropRect{}, false
	}
	return cropRect{
		x: clampInt(values[2], 0, maxInt(0, videoWidth-2)),
		y: clampInt(values[3], 0, maxInt(0, videoHeight-2)),
		w: evenInt(clampInt(values[0], 2, maxInt(2, videoWidth-values[2]))),
		h: evenInt(clampInt(values[1], 2, maxInt(2, videoHeight-values[3]))),
	}, true
}

func splitDimension(input string) []string {
	input = strings.ReplaceAll(input, "X", "x")
	return strings.Split(input, "x")
}

func startEncoding(inputFile string, targetMB float64, resInput string, fpsInput string, trimStart, trimEnd, cropInput, customOut string, hw hwType, codecCfg codecInfo, progressChan chan progressMsg, mode outputMode, quality int, crfSlider int) tea.Cmd {
	return func() tea.Msg {
		defer close(progressChan)

		progressChan <- progressMsg{line: "Analyzing file...", progress: 0}
		info, err := probeFile(inputFile)
		if err != nil {
			return workDoneMsg{err: err}
		}

		duration, _ := strconv.ParseFloat(info.Format.Duration, 64)

		if trimStart != "" && trimEnd != "" {
			s, err := parseDurationStrict(trimStart)
			if err != nil {
				return workDoneMsg{err: err}
			}
			e, err := parseDurationStrict(trimEnd)
			if err != nil {
				return workDoneMsg{err: err}
			}
			if e > s {
				duration = e - s
			} else {
				return workDoneMsg{err: fmt.Errorf("trim end must be after trim start")}
			}
		}

		var outputFile string
		var formatArgs []string
		outputExt := codecCfg.Ext
		switch mode {
		case modeAPNG:
			outputExt = ".png"
		case modeAVIF:
			outputExt = ".avif"
		}

		if customOut != "" {
			outputFile = customOut
			var fmtFlag string
			switch mode {
			case modeAVIF:
				fmtFlag = "avif"
			case modeAPNG:
				fmtFlag = "apng"
			default:
				fmtFlag = strings.TrimPrefix(outputExt, ".")
			}
			formatArgs = []string{"-f", fmtFlag}
		} else {
			dir := filepath.Dir(inputFile)
			name := strings.TrimSuffix(filepath.Base(inputFile), filepath.Ext(inputFile))
			outputFile = filepath.Join(dir, fmt.Sprintf("%s_compressed%s", name, outputExt))
		}

		// allow streaming
		if codecCfg.Ext == ".mp4" {
			formatArgs = append(formatArgs, "-movflags", "+faststart")
		}

		cropFilter, err := buildCropFilter(cropInput)
		if err != nil {
			return workDoneMsg{err: err}
		}
		scaleFilter := buildScaleFilter(resInput)

		vfFilters := []string{}
		if cropFilter != "" {
			vfFilters = append(vfFilters, cropFilter)
		}
		if scaleFilter != "" {
			vfFilters = append(vfFilters, scaleFilter)
		}
		vfFilters = append(vfFilters, "mpdecimate") // remove duplicate frames
		if fpsInput != "" {
			vfFilters = append(vfFilters, fmt.Sprintf("fps=%s", fpsInput))
		}

		vfString := strings.Join(vfFilters, ",")

		trimArgs := []string{}
		if trimStart != "" && trimEnd != "" {
			trimArgs = []string{"-ss", trimStart, "-to", trimEnd}
		}

		switch mode {
		case modeGIF:
			gifVf := []string{}
			if cropFilter != "" {
				gifVf = append(gifVf, cropFilter)
			}
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
			)
			encArgs = append(encArgs, formatArgs...)
			encArgs = append(encArgs, outputFile)

			fullCmd := fmt.Sprintf("ffmpeg %s", strings.Join(encArgs, " "))
			progressChan <- progressMsg{debugCmd: fullCmd}

			if err := runFFmpeg(encArgs, progressChan, duration, "GIF Encode"); err != nil {
				return workDoneMsg{err: err}
			}

			return finishWork(outputFile)

		case modeAPNG:
			progressChan <- progressMsg{line: "Encoding APNG...", progress: 0.1}
			apngVf := []string{}
			if cropFilter != "" {
				apngVf = append(apngVf, cropFilter)
			}
			if scaleFilter != "" {
				apngVf = append(apngVf, scaleFilter)
			}
			apngVf = append(apngVf, "mpdecimate")
			if fpsInput != "" {
				apngVf = append(apngVf, fmt.Sprintf("fps=%s", fpsInput))
			}
			vfString := strings.Join(apngVf, ",")
			encArgs := []string{"-y"}
			encArgs = append(encArgs, trimArgs...)
			encArgs = append(encArgs, "-i", inputFile)
			if vfString != "" {
				encArgs = append(encArgs, "-vf", vfString)
			}
			encArgs = append(encArgs, "-c:v", "apng", "-plays", "0", "-f", "apng")
			encArgs = append(encArgs, formatArgs...)
			encArgs = append(encArgs, outputFile)
			fullCmd := fmt.Sprintf("ffmpeg %s", strings.Join(encArgs, " "))
			progressChan <- progressMsg{debugCmd: fullCmd}
			if err := runFFmpeg(encArgs, progressChan, duration, "APNG Encode"); err != nil {
				return workDoneMsg{err: err}
			}
			return finishWork(outputFile)
		}

		// video & avif mode
		hasAudio := false
		for _, s := range info.Streams {
			if s.CodecType == "audio" {
				hasAudio = true
				break
			}
		}

		isCRFMode := targetMB <= 0
		var videoKBit int

		if !isCRFMode {
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
			videoKBit = int(videoRate / 1024)
		}

		isCPU := hw == hwCPU

		var audioArgs []string
		if hasAudio && mode != modeAVIF {
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

			extraArgs := []string{"-pix_fmt", "yuv420p"}
			if mode == modeAVIF {
				extraArgs = append(extraArgs, "-still-picture", "0")
			}
			switch codecCfg.FFmpegLib {
			case "libvpx-vp9":
				vp9Speeds := []string{"8", "7", "6", "4", "1"}
				extraArgs = append(extraArgs, "-speed", vp9Speeds[quality], "-row-mt", "1", "-tile-columns", "2")
				if isCRFMode {
					crf := 20 + int(float64(crfSlider)*2.5) // 20-45
					extraArgs = append(extraArgs, "-crf", strconv.Itoa(crf), "-b:v", "0")
				}
			case "libaom-av1":
				aomSpeeds := []string{"8", "7", "6", "4", "3"}
				extraArgs = append(extraArgs, "-cpu-used", aomSpeeds[quality], "-row-mt", "1", "-tiles", "2x2")
				if isCRFMode {
					crf := 20 + (crfSlider * 3) // 20-50
					extraArgs = append(extraArgs, "-crf", strconv.Itoa(crf))
				}
			case "libsvtav1":
				svtPresets := []string{"12", "10", "8", "6", "4"}
				extraArgs = append(extraArgs, "-preset", svtPresets[quality])
				if isCRFMode {
					crf := 20 + (crfSlider * 3) // 20-50
					extraArgs = append(extraArgs, "-crf", strconv.Itoa(crf))
				}
			case "librav1e":
				ravSpeeds := []string{"10", "8", "6", "4", "2"}
				extraArgs = append(extraArgs, "-speed", ravSpeeds[quality])
				if isCRFMode {
					crf := 60 + (crfSlider * 8) // 60-140
					extraArgs = append(extraArgs, "-crf", strconv.Itoa(crf))
				}
			case "libx264":
				x264Presets := []string{"ultrafast", "veryfast", "faster", "medium", "veryslow"}
				extraArgs = append(extraArgs, "-preset", x264Presets[quality])
				if isCRFMode {
					crf := 18 + int(float64(crfSlider)*1.5) // 18-33
					extraArgs = append(extraArgs, "-crf", strconv.Itoa(crf))
				}
			case "libx265":
				x265Presets := []string{"ultrafast", "veryfast", "fast", "medium", "veryslow"}
				extraArgs = append(extraArgs, "-preset", x265Presets[quality])
				if isCRFMode {
					crf := 20 + int(float64(crfSlider)*1.6) // 20-36
					extraArgs = append(extraArgs, "-crf", strconv.Itoa(crf))
				}
			default:
				extraArgs = append(extraArgs, "-preset", "medium")
			}

			if isCRFMode {
				// single pass (CRF)
				args := []string{"-y"}
				args = append(args, trimArgs...)
				args = append(args, "-i", inputFile, "-c:v", codecCfg.FFmpegLib)
				args = append(args, extraArgs...)
				args = append(args, filterArgs...)
				args = append(args, audioArgs...)
				args = append(args, formatArgs...)
				args = append(args, outputFile)

				fullCmd := fmt.Sprintf("ffmpeg %s", strings.Join(args, " "))
				progressChan <- progressMsg{debugCmd: fullCmd}

				if err := runFFmpeg(args, progressChan, duration, "Encoding (CRF)"); err != nil {
					return workDoneMsg{err: err}
				}
			} else {
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

				fullCmd1 := fmt.Sprintf("ffmpeg %s", strings.Join(p1, " "))
				progressChan <- progressMsg{debugCmd: fullCmd1}

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
				p2 = append(p2, formatArgs...)
				p2 = append(p2, outputFile)

				fullCmd2 := fmt.Sprintf("ffmpeg %s", strings.Join(p2, " "))
				progressChan <- progressMsg{debugCmd: fullCmd2}

				if err := runFFmpeg(p2, progressChan, duration, "Pass 2 (Encoding)"); err != nil {
					return workDoneMsg{err: err}
				}
				_ = os.Remove(passLog + "-0.log")
				_ = os.Remove(passLog + ".log")
				_ = os.Remove(passLog + "-0.log.mbtree")
			}

		} else {
			extraArgs := []string{"-pix_fmt", "yuv420p"}
			if mode == modeAVIF {
				extraArgs = append(extraArgs, "-still-picture", "0")
			}
			hwQuality := 19 + int(float64(crfSlider)*1.5) // 19-34

			if strings.Contains(codecCfg.FFmpegLib, "nvenc") {
				nvPresets := []string{"p1", "p2", "p4", "p6", "p7"}
				extraArgs = append(extraArgs, "-preset", nvPresets[quality])
				if isCRFMode {
					extraArgs = append(extraArgs, "-rc", "vbr", "-cq", strconv.Itoa(hwQuality))
				} else {
					extraArgs = append(extraArgs, "-rc", "vbr", "-cq", "0")
				}
			} else if strings.Contains(codecCfg.FFmpegLib, "amf") {
				amfPresets := []string{"speed", "speed", "balanced", "quality", "quality"}
				if strings.Contains(codecCfg.FFmpegLib, "av1") {
					amfPresets = []string{"speed", "balanced", "quality", "high_quality", "high_quality"}
				}
				extraArgs = append(extraArgs, "-quality", amfPresets[quality])
				if isCRFMode {
					extraArgs = append(extraArgs, "-rc", "cqp", "-qp_i", strconv.Itoa(hwQuality), "-qp_p", strconv.Itoa(hwQuality))
				}
			} else if strings.Contains(codecCfg.FFmpegLib, "qsv") {
				qsvPresets := []string{"veryfast", "faster", "balanced", "slow", "veryslow"}
				extraArgs = append(extraArgs, "-preset", qsvPresets[quality])
				if isCRFMode {
					extraArgs = append(extraArgs, "-global_quality", strconv.Itoa(hwQuality))
				}
			}

			cmdArgs := []string{"-y", "-hwaccel", "auto"}
			cmdArgs = append(cmdArgs, trimArgs...)
			cmdArgs = append(cmdArgs, "-i", inputFile, "-c:v", codecCfg.FFmpegLib)
			if !isCRFMode {
				cmdArgs = append(cmdArgs,
					"-b:v", fmt.Sprintf("%dk", videoKBit),
					"-maxrate", fmt.Sprintf("%dk", videoKBit),
					"-bufsize", fmt.Sprintf("%dk", videoKBit*2),
				)
			}
			cmdArgs = append(cmdArgs, filterArgs...)
			cmdArgs = append(cmdArgs, extraArgs...)
			cmdArgs = append(cmdArgs, audioArgs...)
			cmdArgs = append(cmdArgs, formatArgs...)
			cmdArgs = append(cmdArgs, outputFile)

			fullCmd := fmt.Sprintf("ffmpeg %s", strings.Join(cmdArgs, " "))
			progressChan <- progressMsg{debugCmd: fullCmd}

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
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (info *FFProbeOutput) videoDimensions() (int, int) {
	if info == nil {
		return 0, 0
	}
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && stream.Width > 0 && stream.Height > 0 {
			return stream.Width, stream.Height
		}
	}
	return 0, 0
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
	fmt.Println("  -apng               Encode to animated PNG")
	fmt.Println("  -avif               Encode to animated AVIF")
	fmt.Println("  -o [file]           Output file path")
	fmt.Println("  -v                  Verbose mode (show command)")
	fmt.Println("  -trim [start] [end] Trim video (e.g. -trim 00:01:00 00:02:00 or -trim 1s 5s)")
	fmt.Println("  -crop [crop]        Crop video (square, 1280x720, 1280x720+0+0, or crop=W:H:X:Y)")
	fmt.Println("  -h, --help, ?       Show this help message")
}

func main() {
	outputMode := modeVideo
	formatFlags := 0
	for _, arg := range os.Args {
		if arg == "-h" || arg == "--help" || arg == "?" {
			printHelp()
			os.Exit(0)
		}
		if arg == "-gif" {
			outputMode = modeGIF
			formatFlags++
		}
		if arg == "-apng" {
			outputMode = modeAPNG
			formatFlags++
		}
		if arg == "-avif" {
			outputMode = modeAVIF
			formatFlags++
		}
	}

	if formatFlags > 1 {
		fmt.Println(errStyle.Render("Error: -gif, -apng, and -avif flags are mutually exclusive."))
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel(outputMode), tea.WithAltScreen(), tea.WithMouseCellMotion())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if m, ok := finalModel.(model); ok {
		switch m.state {
		case stateDone:
			fmt.Println(doneStyle.Render("Success!"))
			fmt.Printf("\nSaved to:\n%s\n%s\n", m.outputFile, m.finalSize)
		case stateError:
			fmt.Println(errStyle.Render("Failed."))
			if m.err != nil {
				fmt.Printf("%v\n", m.err)
			}
		}
	}
}
