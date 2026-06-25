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

You can use Teacrush through the terminal or by creating a desktop shortcut you can simply drag the videos you want to compress on top of it (on Windows, at least)

The TUI includes draggable trim and crop editors with inline frame previews. In the trim editor, drag the range slider handles with the mouse, or use Left/Right to move the active trim handle, PgUp/PgDn to jump farther, Tab to switch between start and end, R to reset the range, and Enter to continue. In the crop editor, drag inside the crop box to move it, drag one of the corner or side-center handles to resize, or use arrows to move and Shift/Ctrl+arrows to resize. Q makes a square crop, R resets, N disables crop, and Enter continues. The preview scales up to fit the terminal and uses higher-quality Lanczos sampling.

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
