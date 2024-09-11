package hls

type TSInfo struct {
	hasVideo       bool
	hasKeyFrame    bool
	hasSetFirstTs  bool
	firstTimestamp int32
	lastTimestamp  int32
}

func NewTSInfo() *TSInfo {
	return &TSInfo{
		hasSetFirstTs: false,
	}
}

func (t *TSInfo) Update(isVideo bool, isKeyFrame bool, timestamp int32) {
	if isVideo {
		t.hasVideo = true
	}
	if isKeyFrame {
		t.hasKeyFrame = true
	}
	if !t.hasSetFirstTs {
		t.hasSetFirstTs = true
		t.firstTimestamp = timestamp
	}
	if t.firstTimestamp > timestamp {
		t.firstTimestamp = timestamp
	}

	t.lastTimestamp = timestamp
}

func (t *TSInfo) Reset() {
	t.hasVideo = false
	t.hasKeyFrame = false
	t.hasSetFirstTs = false
}

func (t *TSInfo) DurationMs() int32 {
	return t.lastTimestamp - t.firstTimestamp
}

func (t *TSInfo) HasVideo() bool {
	return t.hasVideo
}

func (t *TSInfo) HasKeyFrame() bool {
	return t.hasKeyFrame
}

func (t *TSInfo) IsEmpty() bool {
	if t.hasSetFirstTs {
		return false
	}
	return true
}
