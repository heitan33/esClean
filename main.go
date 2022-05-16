package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	elastic "gopkg.in/olivere/elastic.v5"
)

//定义配置文件数据结构
var config Config

type Config struct {
	UrlData []UrlData `json:"data"`
}

type UrlData struct {
	Host       string `json:"host"`
	DiskName   string `json:"diskName"`
	StageHigh  int64  `json:"stage-high"`
	StageLow   int64  `json:"stage-low"`
	IndexWhite string `json:"index-white"`
}

//配置文件初始化
func init() {
//	configFile := flag.String("config-file", "./auth.json", "Path to json file containing emq credentials")
	configFile := flag.String("config-file", "./config.json", "Path to json file containing emq credentials")
	flag.Parse()
//	JsonParse := NewJsonStruct()
	JsonParse := &JsonStruct{}
	JsonParse.Load(*configFile, &config)
}

type JsonStruct struct {

}

//func NewJsonStruct() *JsonStruct {
//	return &JsonStruct{}
//}

func (jst *JsonStruct) Load(filename string, v interface{}) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Println(err)
		return
	}

	err = json.Unmarshal(data, v)
	if err != nil {
		fmt.Println("123")
		fmt.Println(err)
		return
	}
}

var client *elastic.Client

func GetEsClient(urldata UrlData) *elastic.Client {
	file := "./eslog.log"
	logFile, _ := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0766)
	client, err := elastic.NewClient(
		elastic.SetURL(urldata.Host),
		elastic.SetSniff(false),
		elastic.SetInfoLog(log.New(logFile, "ES-INFO: ", 0)),
		elastic.SetTraceLog(log.New(logFile, "ES-TRACE: ", 0)),
		elastic.SetErrorLog(log.New(logFile, "ES-ERROR: ", 0)),
	)
	if err != nil {
		return nil
	}
	return client
}

//删除索引
func DelIndex(urldata UrlData, index ...string) bool {
	client := GetEsClient(urldata)
	response, err := client.DeleteIndex(index...).Do(context.Background())
	defer client.Stop()

	if err != nil {
		fmt.Printf("delete index failed, err: %v\n", err)
		return false
	}
	return response.Acknowledged
}

var esDataList []esData

type esData struct {
	Index string `json:"index"`
	Docs  string `json:"docs.count"`
	Store string `json:"store.size"`
}

//通过接口获取es当前所有index
func post(url string) []byte {
	url = url + "/_cat/indices?format=json&index=*&h=index,docs.count,store.size"
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {

	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	defer func() {
		if err := recover(); err != nil {
			log.Println(err)
			return
		}
		log.Println("Process panic done Post")
	}()
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {

	}

	start := time.Now()
	log.Println(start.Format(time.RFC850))
	log.Println(resp.StatusCode)
	return body
}

//按索引容量大小排序
func bubbleSort(esdata []esData) []esData {
	var swapped = true
	j := 0
	for swapped {
		swapped = false
		for i := 1; i < len(esdata)-j; i++ {
			currentInt64, _ := strconv.ParseInt(esdata[i-1].Docs, 10, 64)
			nextInt64, _ := strconv.ParseInt(esdata[i].Docs, 10, 64)
			if currentInt64 > nextInt64 || nextInt64 == 0 {
				esdata[i], esdata[i-1] = esdata[i-1], esdata[i]
				swapped = true
			}
		}
		j++
	}
	return esdata
}

//判断白名单,并调用删除index函数
func judgeWhiteItem(sortResult []esData) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(30*time.Second))
	var wg sync.WaitGroup
	for _, datum := range config.UrlData {
		sortList := sortResult
		wg.Add(1)
		go func(datum UrlData, ctx context.Context, sortList []esData, wg *sync.WaitGroup) {
			defer wg.Done()
			var stat SystemInfo

			diskName := datum.DiskName
			stat.Commandline = fmt.Sprintf("df -P -h|awk '{print $1, $(NF-1)}'|sed '1d'|grep -w %s", diskName)
			command := stat.Commandline
			for k := range sortList {
				logStore := sortList[len(sortList)-1-k]
				fmt.Println("------------------------")
				fmt.Println("当前go程数量", runtime.NumGoroutine())
				select {
				case <-ctx.Done():
					fmt.Println("本次删除索引结束.")
					return

				default:
					fmt.Println("生成白名单列表.")
					var whiteIndexNameList []string
					for _, indexName := range strings.Split(datum.IndexWhite, ",") {
						whiteIndexNameList = append(whiteIndexNameList, indexName)
					}

					fmt.Println(whiteIndexNameList)
					var deal bool = false
					for _, headIndexName := range whiteIndexNameList {
						if strings.Contains(logStore.Index, headIndexName) {
							deal = true
						}
					}
					if deal == false {
						fmt.Println("delete: "+logStore.Index+", ", "index size: "+logStore.Store)
						DelIndex(datum, logStore.Index)
						diskStat := getDiskHardwareInfo(command)
						diskContentStr := diskStat[1]
						fmt.Println("当前磁盘占用百分比: ", diskContentStr)
						diskContentStr = diskContentStr[0 : len(diskContentStr)-1]
						diskContent, _ := strconv.ParseInt(diskContentStr, 10, 64)
						if diskContent > datum.StageLow {
							break
						} else {
							fmt.Println("磁盘空间已到达目标值.")
							return
						}
					} else {
						fmt.Println(logStore.Index + " 在白名单内,跳过.\n")
						break
					}
				}
			}
		}(datum, ctx, sortList, &wg)
	}
	wg.Wait()
	cancel()
	return
}

type SystemInfo struct {
	Commandline string
}

func (s SystemInfo) getSysInfo() string {
	cmd := exec.Command("/bin/bash", "-c", s.Commandline)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Error:can not obtain stdout pipe for command:%s\n", err)
	}

	if err := cmd.Start(); err != nil {
		log.Println("Error:The command is err,", err)
	}

	bytes, err := ioutil.ReadAll(stdout)
	if err != nil {
		log.Println("ReadAll Stdout:", err.Error())
	}

	if err := cmd.Wait(); err != nil {
		log.Println("wait:", err.Error())
	}

	result := string(bytes)
	return (result)
}

//获取es所有index
func getEsIndexList(diskContent int64, urldata UrlData) (sortResult []esData) {
	fmt.Printf("es地址: %s", urldata.Host)
	fmt.Println("\n")
	fmt.Println("排序并删除首位index,并判断是否到达下限.")
	JsonListByte := post(urldata.Host)
	if err := json.Unmarshal([]byte(JsonListByte), &esDataList); err != nil {
		fmt.Println(err)
	}
	sortResult = bubbleSort(esDataList)
	fmt.Println(diskContent, urldata.DiskName, urldata.StageLow, "\n")
	return sortResult
}

//获取目标磁盘的使用率
func getDiskHardwareInfo(command string) []string {
	var stat SystemInfo
	stat.Commandline = command
	diskStr := strings.TrimSpace(stat.getSysInfo())
	fmt.Println(diskStr)
	fmt.Printf("\n")
	diskStr = strings.Replace(diskStr, "\n", "", -1)
	diskStat := strings.Split(diskStr, " ")
	return diskStat
}

func main() {
	var stat SystemInfo
	for _, datum := range config.UrlData {
		fmt.Println("目标磁盘: ", datum.DiskName)
		fmt.Println("磁盘告警值: ", datum.StageHigh)
		fmt.Println("es host: ", datum.Host)
		stat.Commandline = fmt.Sprintf("ip addr")

		host := string(datum.Host)
		elasticIpList := strings.Split(host, ":")[1]
		elasticIp := string(elasticIpList)[2:len(string(elasticIpList))]
		fmt.Println(elasticIp)
		if strings.Contains(stat.getSysInfo(), elasticIp) == false {
			panic("es host must same with the server ip.")
		}

		if (datum.StageHigh >= 99) || (datum.StageLow >= 99) {
			panic("the value of stage must be less than 99.")
		}

		if datum.StageHigh <= datum.StageLow {
			panic("stage-high must be bigger than stage-low,quit!")
		} else {
			diskName := datum.DiskName
			stat.Commandline = fmt.Sprintf("df -P -h|awk '{print $1, $(NF-1)}'|sed '1d'|grep -w %s", diskName)
			command := stat.Commandline

			for _, datum := range config.UrlData {
				for {
					fmt.Println("获取es磁盘信息.")
					diskStat := getDiskHardwareInfo(command)
					if len(diskStat[0]) > 0 {
						diskContentStr := diskStat[1]
						diskContentStr = diskContentStr[0 : len(diskContentStr)-1]
						diskContent, _ := strconv.ParseInt(diskContentStr, 10, 64)
						if diskContent >= datum.StageHigh {
							indexSortList := getEsIndexList(diskContent, datum)
							fmt.Println(indexSortList)
							judgeWhiteItem(indexSortList)
						} else {
							fmt.Println("磁盘使用率未达到stage-high.")
						}
					} else {
						panic("diskName is not exit,Please check it.")
					}

					ticker := time.NewTicker(5 * time.Minute)
					for range ticker.C {
						fmt.Println("定时结束，执行下一次es盘信息获取.")
						break
					}
				}
			}
		}
	}
}
