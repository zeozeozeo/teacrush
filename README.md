# Teacrush

A [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI app for compressing videos down to a certain size. Basically like [8mb.video](https://8mb.video/) but locally.

https://github.com/user-attachments/assets/dbc959b8-cb32-4400-8369-b703c0f47ad0

You need [FFmpeg](https://www.ffmpeg.org/download.html) installed for this to work.

> [!NOTE]
> Do not use the FFmpeg that comes bundled with your Python installation with this (you can check that by running `which ffmpeg` or `where ffmpeg` on Windows).
> If it is located in the Python installation directory, make sure you have your FFmpeg build higher than Python in PATH.

## Installation

Install [Go](https://go.dev/dl/) if you haven't already.

```console
go install github.com/zeozeozeo/teacrush@latest
```

## Usage

```
$ teacrush -h
Teacrush

Usage:
teacrush [input_file] [flags]

Flags:
-gif                Encode to GIF
-trim [start] [end] Trim video (e.g. -trim 00:01:00 00:02:00 or -trim 1s 5s)
-h, --help, ?       Show this help message
```
