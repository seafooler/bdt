package core

const HASHSIZE = 32

// NewTxBatch creates a new transaction batch, consisting of transaction hashes
func NewTxBatch(max_payload_size int) []byte {
	var payload = make([]byte, max_payload_size)
	for i, _ := range payload {
		payload[i] = 0
	}
	return payload
}
