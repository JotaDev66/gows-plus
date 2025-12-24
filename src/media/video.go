package media

import (
	"bytes"
	"fmt"
	"github.com/u2takey/ffmpeg-go"
	"os"
)

// VideoThumbnail generates a thumbnail image from a video at a specific frame.
func VideoThumbnail(content []byte, frameNum int, size struct{ Width int }) ([]byte, error) {
	inputReader := bytes.NewReader(content)
	var buf bytes.Buffer
	cmd := ffmpeg_go.Input("pipe:0").
		Filter("scale", ffmpeg_go.Args{fmt.Sprintf("%d:-1", size.Width)}).
		Filter("select", ffmpeg_go.Args{fmt.Sprintf("gte(n,%d)", frameNum)}).
		Output("pipe:", ffmpeg_go.KwArgs{"vframes": 1, "format": "image2"}).
		WithInput(inputReader).
		WithOutput(&buf).
		WithErrorOutput(os.Stderr).
		OverWriteOutput()
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	data := buf.Bytes()
	if len(data) == 0 {
		return nil, fmt.Errorf("no thumbnail data returned")
	}
	return buf.Bytes(), nil
}
