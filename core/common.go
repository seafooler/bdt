package core

const HASHSIZE = 32

// NewTxBatch creates a new transaction batch, consisting of transaction hashes
func NewTxBatch(txNum, txSize int) []byte {
	if txNum > 1 {
		var payload = make([]byte, txNum*txSize)
		payload[txNum*txSize-1] = 1
		return payload
	} else {
		return nil
	}
	return nil
}
