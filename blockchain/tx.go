package blockchain

import (
	"blockchain_go/common"
	"blockchain_go/wallet"
	"bytes"
	"encoding/gob"
)

// TxOutput, 在 UTXO 模型下，每个输出就是一笔未花费的“钱”，可以被某个公钥解锁。
type TxOutput struct{
	Value int     // 输出金额（token 数量）
	PubKeyHash []byte // 对应拥有者的公钥或地址（表示谁能花这笔钱）
}
// TxInput, 输入就是“花钱的凭证”，指向某个未花费输出。
type TxInput struct{
	ID []byte  // 引用的 前一笔交易的 TxID（也就是你要花的那笔输出所在的交易）
	Out int    // 前一笔交易中输出的索引（哪一个输出被花掉）
	Signature []byte  // 签名或数据，用于证明你有权花掉这个输出
	PubKey    []byte
}

type TxOutputs struct {
	Outputs []TxOutput
}

/*
In Bitcoin’s transaction model:
A transaction output (TxOutput) is “locked” to a public key hash (pubKeyHash).
To spend this output (UTXO), a transaction input (TxInput) must provide a valid
signature and public key to “unlock” it.
*/

func (in *TxInput) UsesKey(pubKeyHash []byte) bool {
	lockingHash := wallet.PublicKeyHash(in.PubKey)

	return bytes.Equal(lockingHash, pubKeyHash)
}

func (out *TxOutput) Lock(address []byte) {
	pubKeyHash := wallet.Base58Decode(address)
	pubKeyHash = pubKeyHash[1 : len(pubKeyHash)-4]
	out.PubKeyHash = pubKeyHash
}

func (out *TxOutput) IsLockedWithKey(pubKeyHash []byte) bool {
	return bytes.Equal(out.PubKeyHash, pubKeyHash)
}

func NewTXOutput(value int, address string) *TxOutput {
	txo := &TxOutput{value, nil}
	txo.Lock([]byte(address))
	return txo
}

func (outs TxOutputs) Serialize() []byte {
	var buffer bytes.Buffer

	encode := gob.NewEncoder(&buffer)
	err := encode.Encode(outs)
	common.HandlerError(err)

	return buffer.Bytes()
}

func DeserializeOutputs(data []byte) TxOutputs {
	var outputs TxOutputs

	decode := gob.NewDecoder(bytes.NewReader(data))
	err := decode.Decode(&outputs)
	common.HandlerError(err)
	return outputs
}