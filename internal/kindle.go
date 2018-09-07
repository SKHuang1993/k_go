package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
)

var Map map[string]string

type Note struct {
	Name     string `json:"name"`
	Time     string `json:"time"`
	Content  string `json:"content"`
	Location string `json:"location"`
}

func init() {

	Map = make(map[string]string)

}

//#TODO 当插入kindle时，运行我们的二进制程序，同时在后面加上目标路径。自动抽取每个笔记的内容，建立对应的文件（后期加上）

func main() {

	// var m = make(map[string]interface{})

	//以这个文件为教程，在我的博客上发表第一篇文章

	//读取文件
	file, err := os.Open("kindleClip.txt")
	if err != nil {
		log.Fatal("打开文件失败")

	}
	defer file.Close()

	content, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatal("文件转字节失败")

	}

	//fmt.Println(string(content))

	//遍历这个文件（每个笔记都是以"======="来分割的。第一行为书名，第二行为标注的位置，接下来是空格。第四行开始则为笔记内容）

	steps := strings.Split(string(content), "==========")

	for _, item := range steps {

		//fmt.Println(item)

		//将每个笔记去掉所有的换行符
		newItem := strings.Split(item, "\n")

		currentItem := RemoveEmpty(newItem)

		if len(currentItem) > 2 {

			bookName := currentItem[1]

			_, ok := Map[bookName]

			if ok {

				//fmt.Println("已经有这个书名了，直接写在原来的文件里面")

			} else {

				fmt.Println(bookName)

				filename := "Note/" + bookName + ".txt"

				file1, err := os.Create(filename)
				if err != nil {
					log.Fatalln("创建文件失败")
				}
				defer file1.Close()

				//接着往文件里面写入内容
				file.Write([]byte(currentItem[2]))
				file.Write([]byte(currentItem[3]))
				//file.Write([]byte(currentItem[4]))

				//书名为key，文件名为对应的value
				Map[bookName] = filename

				//创建书名文件，接着往里面写数据

			}

		}

		/*
			for index, s := range currentItem {

					fmt.Println(index,"====",s)

				// fmt.Println(len(newItem))

			}
		*/

	}

	//	item := strings.Split(steps[100],"\n")

	//for item :=range steps{
	//
	//
	//
	//}

}

func RemoveEmpty(old []string) (current []string) {

	length := len(old)

	for i := 0; i < length; i++ {

		if len(old[i]) == 0 {
			continue
		}

		current = append(current, old[i])

	}

	return

}
