
package kklog

import (
	"strings"
	"io/ioutil"
)

type Color int

const(
	NoColor Color = iota
	Black
	Red
	Green
	Yellow
	Blue
	Purple
	DarkGreen
	White
)



var logColorPrefix = []string{
	"",
	"\x1b[030m",
	"\x1b[031m",
	"\x1b[032m",
	"\x1b[033m",
	"\x1b[034m",
	"\x1b[035m",
	"\x1b[036m",
	"\x1b[037m",
}


type ColorData struct {
	name string
	c Color
}

var colorByName  =[]ColorData{

	{"none",NoColor},
	{"black", Black},
	{"red", Red},
	{"green", Green},
	{"yellow", Yellow},
	{"blue", Blue},
	{"purple", Purple},
	{"darkgreen", DarkGreen},
	{"white", White},
}


//传入颜色的字符串，返回对应的颜色int
func matchColor(name string) Color  {

	lower := strings.ToLower(name)

	for _,d := range colorByName{

		if d.name == lower{
			return d.c
		}

	}
	return NoColor
}

func colorFromLevel(l Level)Color  {

	switch l {

	case Level_Debug:
		return Yellow
	case Level_Error:
		return Red
	default:
		return NoColor
	}
	return NoColor

}


var logColorSuffix ="\x1b[0m"


func SetColorDefine(loggerName string, jsonFormat string)error  {

	cf :=NewColorFile()
	if err := cf.Load(jsonFormat);err!=nil{
		return err
	}

	return VisitLogger(loggerName, func(l *Logger) bool {

		l.SetColorFile(cf)
		return  true

	})
}

func EnableColorLogger(loggerName string, enable bool) error  {


	return VisitLogger(loggerName, func(l *Logger) bool {

		l.EnableColor(enable)
		return  true
	})

}


func setColorFile(loggerName string, colorFileName string) error  {

	data,err := ioutil.ReadFile(colorFileName)
	if err !=nil{
		return  err
	}
	return  SetColorDefine(loggerName,string(data))

}






