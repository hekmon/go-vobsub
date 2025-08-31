package vobsub

const (
	subtitleCTRLSeqCmdForceDisplaying = 0x00
	subtitleCTRLSeqCmdStartDate       = 0x01
	subtitleCTRLSeqCmdStopDate        = 0x02
	subtitleCTRLSeqCmdPalette         = 0x03
	subtitleCTRLSeqCmdAlphaChannel    = 0x04
	subtitleCTRLSeqCmdCoordinates     = 0x05
	subtitleCTRLSeqCmdRLEOffsets      = 0x06
	subtitleCTRLSeqCmdEnd             = 0xff
)

type Subtitle struct {
	data []byte
}
