package bucket

type BucketOption func(ch *Bucket)

func WithChannelSize(channelSize int) BucketOption {
	return func(b *Bucket) {
		b.ChannelSize = channelSize
	}
}

func WithRoomSize(roomSize int) BucketOption {
	return func(b *Bucket) {
		b.RoomSize = roomSize
	}
}

func WithRoutineAmount(routineAmount uint64) BucketOption {
	return func(b *Bucket) {
		b.RoutineAmount = routineAmount
	}
}

func WithRoutineSize(routineSize int) BucketOption {
	return func(b *Bucket) {
		b.RoutineSize = routineSize
	}
}
