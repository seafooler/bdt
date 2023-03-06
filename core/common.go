package core

const HASHSIZE = 32

// NewTxBatch creates a new transaction batch, consisting of transaction hashes
func NewTxBatch(txNUm int) []byte {
	var payload = make([]byte, txNUm)
	for i := 0; i < txNUm; i++ {
		payload[i] = 1
	}
	return payload
}
