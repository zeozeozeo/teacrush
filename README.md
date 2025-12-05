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

## Codec caution

H264 is a good fit for simple/static videos. Any potato should be able to encode it fast enough. But if you're compressing gameplay or such, you might want to look into better options.

VP9 is *excruciatingly* slow to encode but widely supported and has way better quality than both HEVC and H264. Firefox will play it, but your CPU will not have a great time encoding it.

HEVC is quite optimized on the CPU and even more so on the GPU, with minute long videos taking seconds to encode. However, most browsers will not play it (except Electon apps, like Discord, but remember that Discord can also be used in a browser).

So if you have time, it is always better to use VP9. Otherwise go with H264 for compatibility or HEVC for quality.
