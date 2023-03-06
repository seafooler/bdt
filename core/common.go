package core

const HASHSIZE = 32

// NewTxBatch creates a new transaction batch, consisting of transaction hashes
func NewTxBatch(txNUm int) []byte {
	var payload = make([]byte, txNUm*HASHSIZE)
	payload[txNUm*512-1] = 1
	return payload
}
