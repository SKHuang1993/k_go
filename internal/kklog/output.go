package kklog

import (
	"os"
	"io"
)

//输出到文件
func SetOutputToFile(loggerName string,fileName string) error  {

	mode := os.O_RDWR | os.O_CREATE | os.O_APPEND

	//打开文件，中间是文件的模式，最后是文件的权限
	file,err := os.OpenFile(fileName,mode,0666)
	if err!=nil{
		return  err
	}

	return  VisitLogger(loggerName, func(l *Logger)bool {

		l.SetOutput(file)
		return true
	})


}

func SetOutput(loggerName string,output io.Writer)error  {

	return VisitLogger(loggerName, func(l *Logger)bool {

		l.SetOutput(output)
		return  true

	})


}