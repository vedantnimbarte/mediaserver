package transcode

import (
	"fmt"
	"math"
	"strings"
)

// SegmentCount returns how many segments a stream of the given duration produces.
func SegmentCount(durationSec float64, segmentSec int) int {
	if durationSec <= 0 || segmentSec <= 0 {
		return 0
	}
	return int(math.Ceil(durationSec / float64(segmentSec)))
}

// SegmentDuration returns the length of segment n, which is shorter for the last one.
func SegmentDuration(n int, durationSec float64, segmentSec int) float64 {
	full := float64(segmentSec)
	start := float64(n) * full
	if start+full <= durationSec {
		return full
	}
	if remainder := durationSec - start; remainder > 0 {
		return remainder
	}
	return full
}

// MasterPlaylist renders the top-level playlist listing every variant.
func MasterPlaylist(ladder []Rendition, variantURL func(Rendition) string) string {
	var b strings.Builder

	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")

	for _, r := range ladder {
		b.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d",
			r.MaxBitrate()+r.AudioBitrate, r.TotalBitrate()))

		if r.Width > 0 && r.Height > 0 {
			b.WriteString(fmt.Sprintf(",RESOLUTION=%dx%d", r.Width, r.Height))
		}
		b.WriteString(fmt.Sprintf(",CODECS=%q\n", codecsAttribute(r)))
		b.WriteString(variantURL(r) + "\n")
	}

	return b.String()
}

// VariantPlaylist renders a VOD playlist listing every segment up front.
//
// The whole list is emitted even though nothing has been encoded yet. That is the
// point of the design: because the player knows the full timeline immediately, it can
// seek anywhere and simply request the segment it lands on, and the server starts
// encoding from there. A live-style rolling playlist would force sequential playback.
func VariantPlaylist(durationSec float64, segmentSec int, segmentURL func(n int) string) string {
	count := SegmentCount(durationSec, segmentSec)

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", segmentSec))
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")

	for n := 0; n < count; n++ {
		b.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", SegmentDuration(n, durationSec, segmentSec)))
		b.WriteString(segmentURL(n) + "\n")
	}

	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}
