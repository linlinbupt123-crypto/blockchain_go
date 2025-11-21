package blockchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"log"
)

func Genesis(coinbase *Transaction) *Block {
	return CreateBlock([]*Transaction{coinbase}, []byte{})
}

type Block struct {
	Hash    []byte
	Transactions    []*Transaction
	PrevHash []byte
	Nonce int
}

func CreateBlock(coinbase []*Transaction, prevHash []byte) *Block {
	block := &Block{
		Hash:    []byte{},
		Transactions:    coinbase,
		PrevHash: prevHash,
		Nonce: 0,
	}
	pow := NewProof(block)
	nonce, hash := pow.Run()
	block.Hash = hash[:]
	block.Nonce = nonce
	return block
}
/*
这个函数会做的事情：

遍历区块里的每一笔交易

序列化交易内容成字节（Bytes）

将所有交易字节拼接在一起

对拼接后的字节数组做一次 哈希运算（比如 SHA-256）

得到的哈希值就代表 整个区块中所有交易的唯一“指纹”。
*/
// HashTransactions:作用是把区块里 所有交易组合起来，生成一个唯一表示。
func (b *Block)HashTransactions() []byte{
	var txHashes [][]byte
	var txHash [32]byte
	for _, tx := range b.Transactions{
		txHashes = append(txHashes, tx.ID)
	}
	txHash = sha256.Sum256(bytes.Join(txHashes, []byte{}))
	return txHash[:]
}

func (b *Block) Serialize() []byte{
	var res bytes.Buffer
	encoder := gob.NewEncoder(&res)
	err := encoder.Encode(b)
	if err != nil {
		log.Panic(err)
	}
	return res.Bytes()
}

func Deserialize(data []byte) *Block{
	var block Block
	decoder := gob.NewDecoder(bytes.NewReader(data))
	err := decoder.Decode(&block)
	if err != nil {
		log.Panic(err)
	}
	return &block
}




