package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

type challenge struct {
	Question string
	Answer   int
}

func newChallenge() (challenge, error) {
	// Use a short three-step expression. It remains solvable by a person while
	// defeating simple scripts that only handle a single addition question.
	a, err := randomInt(3, 20)
	if err != nil {
		return challenge{}, err
	}
	b, err := randomInt(2, 10)
	if err != nil {
		return challenge{}, err
	}
	c, err := randomInt(2, 10)
	if err != nil {
		return challenge{}, err
	}
	d, err := randomInt(1, 10)
	if err != nil {
		return challenge{}, err
	}
	return challenge{
		Question: fmt.Sprintf("(%d + %d) × %d - %d = ?", a, b, c, d),
		Answer:   (a+b)*c - d,
	}, nil
}

func randomInt(min, max int) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min)))
	if err != nil {
		return 0, err
	}
	return min + int(n.Int64()), nil
}
