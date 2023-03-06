package core

const HASHSIZE = 32

// NewTxBatch creates a new transaction batch, consisting of transaction hashes
func NewTxBatch(txNUm int) []byte {
	//if txNUm > 1 {
	//	var payload = make([]byte, txNUm*HASHSIZE)
	//	payload[txNUm*HASHSIZE-1] = 1
	//	return payload
	//} else {
	//	return nil
	//}
	return nil
}
