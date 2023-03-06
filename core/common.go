package core

const HASHSIZE = 32

// NewTxBatch creates a new transaction batch, consisting of transaction hashes
func NewTxBatch(maxPayloadSize int) []byte {
	var payload = make([]byte, maxPayloadSize)
	for i, _ := range payload {
		payload[i] = 0
	}
	return payload
}
