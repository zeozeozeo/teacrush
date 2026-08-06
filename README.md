# Teacrush

A [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI app for compressing videos down to a certain size. Basically like [8mb.video](https://8mb.video/) but locally.

https://github.com/user-attachments/assets/dbc959b8-cb32-4400-8369-b703c0f47ad0

You need [FFmpeg](https://www.ffmpeg.org/download.html) installed for this to work.

> [!NOTE]
> Do not use the FFmpeg that comes bundled with your Python installation with this (you can check that by running `which ffmpeg` or `where ffmpeg` on Windows).
> If it is located in the Python installation directory, make sure you have your FFmpeg build higher than Python in PATH.

## Installation

### Pre-built nightly binaries

Pre-built binaries are published for Linux, macOS, and Windows. These installers do not require Go and can also be run again to update to the latest nightly build.

On Linux or macOS, run:

```console
curl -fsSL https://github.com/zeozeozeo/teacrush/releases/download/nightly/install.sh | sh
```

This installs to `~/.local/bin`. Add that directory to `PATH` if it is not already there. To install for all users instead, set `TEACRUSH_INSTALL_DIR` to a system-wide bin directory such as `/usr/local/bin` and run the command with the permissions required by that directory.

On Windows PowerShell, run:

```powershell
irm https://github.com/zeozeozeo/teacrush/releases/download/nightly/install.ps1 | iex
```

This installs to `%LOCALAPPDATA%\Programs\teacrush` and adds that directory to the user `PATH`. Run the same command again to update it.

To uninstall on Linux or macOS, run:

```console
curl -fsSL https://github.com/zeozeozeo/teacrush/releases/download/nightly/install.sh | sh -s -- uninstall
```

On Windows:

```powershell
$script = Join-Path $env:TEMP 'teacrush-install.ps1'; irm https://github.com/zeozeozeo/teacrush/releases/download/nightly/install.ps1 -OutFile $script; & $script -Uninstall; Remove-Item $script
```

The release also contains archives and a `SHA256SUMS` file for manual installation.

### Install from source

Install [Go](https://go.dev/dl/) if you haven't already.

```console
go install github.com/zeozeozeo/teacrush@latest
```

## Usage

You can use Teacrush through the terminal or by creating a desktop shortcut you can simply drag the videos you want to compress on top of it (on Windows, at least)

```
$ teacrush -h
Teacrush

Usage:
  teacrush [input_file] [flags]

Flags:
  -gif                Encode to GIF
  -apng               Encode to animated PNG
  -avif               Encode to animated AVIF
  -o [file]           Output file path
  -v                  Verbose mode (show command)
  -trim [start] [end] Trim video (e.g. -trim 00:01:00 00:02:00 or -trim 1s 5s)
  -crop [crop]        Crop video (square, 1280x720, 1280x720+0+0, or crop=W:H:X:Y)
  -h, --help, ?       Show this help message
```

## Encoder preset mapping

| Level        | SVT-AV1 | rav1e   | VP9 | AOM-AV1 | H.264 / H.265 | NVENC | AMF (H.264/HEVC) | AMF (AV1)    | QSV      |
| :----------- | :------ | :------ | :-- | :------ | :------------ | :---- | :--------------- | :----------- | :------- |
| **Fastest**  | P12[^1] | S10[^2] | S8  | CPU 8   | ultrafast     | p1    | speed            | speed        | veryfast |
| **Faster**   | P10     | S8      | S7  | CPU 7   | veryfast      | p2    | speed            | balanced     | faster   |
| **Balanced** | P8      | S6      | S6  | CPU 6   | faster        | p4    | balanced         | quality      | balanced |
| **Better**   | P6      | S4      | S4  | CPU 4   | medium        | p6    | quality          | high_quality | slow     |
| **Best**     | P4      | S2      | S1  | CPU 3   | veryslow      | p7    | quality          | high_quality | veryslow |

[^1]: P = `-preset`

[^2]: S = `-speed`

## CRF level mapping

| Encoder / HW | FFmpeg Parameter | Value Range |
| :--- | :--- | :---: |
| **SVT-AV1** | `-crf` | `20 - 50` |
| **AOM-AV1** | `-crf` | `20 - 50` |
| **rav1e** | `-crf` | `60 - 140` |
| **VP9** | `-crf` | `20 - 45` |
| **H.264** | `-crf` | `18 - 33` |
| **H.265** | `-crf` | `20 - 36` |
| **NVENC** | `-cq` | `19 - 34` |
| **AMF** | `-qp_i` / `-qp_p` | `19 - 34` |
| **QSV** | `-global_quality` | `19 - 34` |
