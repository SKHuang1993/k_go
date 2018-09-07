package kklog

import (
	"regexp"
	"sync"
)

var (
	loggerByName      = map[string]*Logger{}
	loggerByNameGuard sync.RWMutex
)

//将日志名添加到缓存中，如已存在则跳过；不存在则新建

func add(l *Logger) {
	loggerByNameGuard.Lock()

	if l.name != "" {

	  if _,ok := loggerByName[l.name];ok{
	  	panic("duplicate logger name: "+l.name)
	  }

	  loggerByName[l.name] = l

	}

	loggerByNameGuard.Unlock()

}

//支持正则表达式查找logger，a|b|c指定多个日志，.表示所有日志
func VisitLogger(names string, callback func(*Logger) bool) error {

	loggerByNameGuard.Lock()
	defer loggerByNameGuard.Unlock()

	exp, err := regexp.Compile(names)
	if err != nil {
		return err
	}

	for _, l := range loggerByName {
		if exp.MatchString(l.Name()) {

			if !callback(l) {
				break
			}

		}

	}
	return nil

}

func ClearAll() {

	loggerByNameGuard.Lock()
	loggerByName = map[string]*Logger{}
	loggerByNameGuard.Unlock()

}
