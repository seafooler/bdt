package core

const HASHSIZE = 32

// NewTxBatch creates a new transaction batch, consisting of transaction
func NewTxBatch(txNUm int) []byte {
	var payload = make([]byte, txNUm*512)
	for i := 0; i < txNUm*512; i++ {
		payload[i] = 1
	}
	return payload
}
