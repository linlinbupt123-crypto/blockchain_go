package common

import (
	"log"
)

func HandlerError(err error){
	if err != nil {
		log.Panic(err)
	}
}