package main

//#include <stdio.h>
import "C"

//void SayHello(const char *s);

func main() {

	//C.puts(C.CString("Hello, World\n"))

	C.SayHello(C.CString("Hello, World\n"))

}
