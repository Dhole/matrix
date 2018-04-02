package main

import (
	"fmt"
	"github.com/boltdb/bolt"
	"os"
	"time"
	"unicode/utf8"
)

func pad(length int) {
	for i := 0; i < length; i++ {
		fmt.Printf(" ")
	}
}
func recurse(b *bolt.Bucket, lvl int) {
	b.ForEach(func(key, value []byte) error {
		pad(lvl * 2)
		fmt.Printf("%s: ", string(key))
		if value != nil {
			var s string
			if !utf8.Valid(value) || len(value) <= 8 {
				s = fmt.Sprintf("%v", value)
			} else {
				s = string(value)
			}
			fmt.Printf("%s\n", s)
		} else {
			fmt.Printf("{\n")
			recurse(b.Bucket(key), lvl+1)
			pad(lvl * 2)
			fmt.Printf("}\n")
		}
		return nil
	})
}

func main() {
	filename := os.Args[1]

	db, err := bolt.Open(filename, 0660, &bolt.Options{Timeout: 200 * time.Millisecond})
	err = db.View(func(tx *bolt.Tx) error {
		tx.ForEach(func(key []byte, b *bolt.Bucket) error {
			fmt.Printf("%s: {\n", string(key))
			recurse(b, 1)
			fmt.Printf("}\n")
			return nil
		})
		return nil
	})
	if err != nil {
		fmt.Println(err)
	}
}
