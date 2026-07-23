package kafkax

type Sender interface {
	Send(topic, key string, value []byte) error
}
