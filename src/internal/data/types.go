package data

import (
	"sync"
)

type SymbolPrice struct {
	Uuid   int64   `json:"uuid"`
	Symbol string  `json:"symbol"`
	Bid    float32 `json:"bid"`
	Ask    float32 `json:"ask"`
}

type Position struct {
	UUID   int64   `json:"uuid"`
	Symbol string  `json:"symbol"`
	Open   float32 `json:"open"`
	Close  float32 `json:"close"`
	IsBay  bool    `json:"isbay"`
}

type LastPrices struct {
	Mu     sync.Mutex
	Values map[string]SymbolPrice
}

type Positions struct {
	Mu     sync.Mutex
	Values map[string]map[int64]Position
}

func (p Position) PNL(lastPrice SymbolPrice) float32 {
	if p.IsBay {
		return p.Open - lastPrice.Bid
	}
	return lastPrice.Ask - p.Open
}

func (l *LastPrices) PushValue(price SymbolPrice) {
	l.Mu.Lock()
	l.Values[price.Symbol] = price
	l.Mu.Unlock()
}

func (p *Positions) PushValue(position Position) {
	p.Mu.Lock()
	p.Values[position.Symbol][position.UUID] = position
	p.Mu.Unlock()
}
