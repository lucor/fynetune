package hls

import (
	"testing"

	"github.com/bluenviron/gohlslib/v2/pkg/codecs"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
)

func TestADTSHeader(t *testing.T) {
	codec := &codecs.MPEG4Audio{Config: mpeg4audio.AudioSpecificConfig{
		Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 44100, ChannelConfig: 2,
	}}
	header, err := adtsHeader(codec, 100)
	if err != nil {
		t.Fatal(err)
	}
	if header[0] != 0xff || header[1] != 0xf1 || header[2] != 0x50 || header[3] != 0x80 || header[4] != 0x0d || header[5] != 0x7f || header[6] != 0xfc {
		t.Fatalf("ADTS header = % x", header)
	}
}

func TestADTSHeaderRejectsUnsupportedConfiguration(t *testing.T) {
	codec := &codecs.MPEG4Audio{Config: mpeg4audio.AudioSpecificConfig{
		Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 44100, ChannelConfig: 3,
	}}
	if _, err := adtsHeader(codec, 10); err == nil {
		t.Fatal("expected unsupported channel configuration error")
	}
}
