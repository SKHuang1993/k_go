package json

import (
	"reflect"
	"testing"
)

type Message struct {
	Code int    `json:"code"`
	Data string `json:"data"`
}

func TestSerialization_MapToByte(t *testing.T) {

	m := Message{1, "hello world"}
	s := NewSerialization()

	d1, err := s.Marshal(m)
	if err != nil {
		t.Fail()
	}

	var m2 Message

	if err := s.Unmarshal(d1, &m2); err != nil {
		t.Fail()
	}

	if !reflect.DeepEqual(m, m2) {
		t.Fail()

	}

}

func TestSerializer_Serialize(t *testing.T) {

	m := Message{1, "hello world"}
	s := NewSerialization()

	b, err := s.Marshal(m)
	if err != nil {
		t.Fail()
	}

	m2 := Message{}
	if err := s.Unmarshal(b, &m2); err != nil {
		t.Fail()
	}

	if !reflect.DeepEqual(m, m2) {
		t.Fail()
	}

}

func BenchmarkSerialization_Deserialize(b *testing.B) {

	m := &Message{100, "hell world"}
	s := NewSerialization()

	d, err := s.Marshal(m)
	if err != nil {
		b.Error(err)
	}

	for i := 0; i < b.N; i++ {
		m1 := &Message{}
		s.Unmarshal(d, m1)

	}

}
