package kklog


type Level int

const(

	Level_Debug Level = iota
	Level_Info
	Level_Warn
	Level_Error

)

var levelString = [...]string{
	"[DEBU]",
	"[INFO]",
	"[WARN]",
	"[ERRO]",

}



//字符串转对应的日记级别
func  str2logLever(levelStr string)Level {

	switch levelStr {

	case "debug":
		return Level_Debug

	case "info":
		return Level_Info

	case "warn":
		return Level_Warn
	case "error","err":
		return Level_Error

	default:
		return Level_Debug

	}

	return Level_Debug

}

//通过字符串设置某一类日志的类别

func SetLeverByString(loggerName string, level string)   error{


	return VisitLogger(loggerName, func(l *Logger)bool {

l.SetLevelByString(level)
return  true

	})


}










