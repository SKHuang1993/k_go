//封装json的方法

package json

import (
	"encoding/json"
	"github.com/pkg/errors"
	"log"
	"reflect"
)

type Serialization struct {

}

//return a new Serialization
func NewSerialization() *Serialization {

	return &Serialization{}
}

func (s *Serialization) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

//将 []byte 转object
func (s *Serialization) Unmarshal(data []byte, v interface{}) error {

	return json.Unmarshal(data, v)

}

//将结构转字典
func (s *Serialization) StructToMap(obj interface{}) map[string]interface{} {

	t := reflect.TypeOf(obj)
	v := reflect.ValueOf(obj)

	var data = make(map[string]interface{})

	for i := 0; i < t.NumField(); i++ {

		data[t.Field(i).Name] = v.Field(i).Interface()

	}
	return data

}

func (s *Serialization) MapToString(m map[string]interface{}) (string, error) {

	data, err := json.Marshal(m)
	if err != nil {
		log.Fatal("MapToString 解析错误")
		return "", errors.New("解析错误")

	}
	return string(data), nil

}

func (s *Serialization) MapToByte(m map[string]interface{}) ([]byte, error) {

	data, err := json.Marshal(m)
	if err != nil {
		return nil, errors.New("MapToByte")
	}
	return data, nil

}
