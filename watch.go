package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	assetfs "github.com/elazarl/go-bindata-assetfs"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/net"
)

var (
	httpserver  *string
	infoContain []sInfo
	ProcessName *string
	byteSent    uint64
	byteRev     uint64
	cpuCors     int
)

type sInfo struct {
	Cpu      int32
	Mem      int32
	Load     int32
	Time     string
	Stamps   int64
	RevByte  uint64
	SendByte uint64
	FsCpu    float64
	FsMem    float64
	FsThread float64
}

func init() {
	httpserver = flag.String("httpserver", "0.0.0.0:888", "httpserver addr")
	ProcessName = flag.String("Pname", "freeswitch", " process name ")
	cpuCors, _ = cpu.Counts(false)
}
func GetCpuPercent() float64 {
	percent, _ := cpu.Percent(time.Second*3, false)
	return percent[0]
}
func GetMemPercent() float64 {
	memInfo, _ := mem.VirtualMemory()
	return memInfo.UsedPercent
}
func getNetInfo() (uint64, uint64) {
	info, _ := net.IOCounters(true)
	var byteSent_tmp, byteRev_tmp uint64
	for _, v := range info {
		byteSent_tmp = byteSent_tmp + v.BytesSent
		byteRev_tmp = byteRev_tmp + v.BytesRecv
	}
	if byteSent == 0 && byteRev == 0 {
		byteSent = byteSent_tmp
		byteRev = byteRev_tmp
		return 0, 0
	}
	ret_send, ret_rev := (byteSent_tmp-byteSent)/1024/3, (byteRev_tmp-byteRev)/1024/3
	byteSent = byteSent_tmp
	byteRev = byteRev_tmp
	return ret_send, ret_rev
}
func shellCommand(command string) string {
	cmd := exec.Command("/bin/bash", "-c", command)
	bytes, err := cmd.Output()
	if err != nil {
		return ""
	} else {
		resp := string(bytes)
		resp = strings.ReplaceAll(resp, "\n", "")
		resp = strings.ReplaceAll(resp, "\r", "")
		return resp
	}
}
func getPid(pname string) string {
	pid := shellCommand(`ps -el|grep freeswitch|grep -v grep|awk '{print $4}'`)
	log.Println(" GetPid ", pname, pid)
	return pid
}

//get process cpu percent, mem percent
func getProcessInfo(pname string) (float64, float64, float64) {
	getinfo_, _ := Asset("getinfo")
	command := strings.ReplaceAll(string(getinfo_), "__PROCESSNAME__", pname)
	command = strings.ReplaceAll(string(command), "__SELF__", os.Args[0])
	//log.Println("shell info:", command)
	info := strings.Split(shellCommand(command), " ")
	if len(info) == 3 {
		mem, _ := strconv.ParseFloat(info[0], 32)
		cpu, _ := strconv.ParseFloat(info[1], 32)
		threads, _ := strconv.ParseFloat(info[2], 32)
		return mem, cpu * 100, threads
	}
	return 0, 0, 0
}
func watch_timer() {
	for {
		var data sInfo
		data.Cpu = int32(GetCpuPercent())
		data.Mem = int32(GetMemPercent())
		data.Time = time.Unix(time.Now().UTC().Unix(), 0).Format("2006-01-02 15:04:05")
		data.Stamps = time.Now().UTC().Unix()
		data.RevByte, data.SendByte = getNetInfo()
		loadState, _ := load.Avg()
		data.Load = int32(loadState.Load1)

		data.FsMem, data.FsCpu, data.FsThread = getProcessInfo(*ProcessName)
		data.FsCpu = data.FsCpu / 100

		infoContain = append(infoContain, data)

		expire_time := time.Now().UTC().Unix() - 3600*12 //save 12h data
		for index, info := range infoContain {
			if info.Stamps < expire_time {
				infoContain = infoContain[index:]
			} else {
				break
			}
		}
	}
}
func main() {
	flag.Parse()
	go watch_timer()
	http_server_run(*httpserver)
	for {
	}
}
func transTime(format string) int64 {
	format = strings.Replace(format, "T", " ", 1)
	format = format + ":00"
	loc, _ := time.LoadLocation("Local")
	timestamp, err := time.ParseInLocation("2006-01-02 15:04:05", format, loc)
	if err == nil {
		return timestamp.Unix()
	} else {
		return 0
	}
}
func http_server_run(httpserver string) {
	//fs := http.FileServer(http.Dir("js"))
	//http.Handle("/js/", http.StripPrefix("/js/", fs))
	fs := assetfs.AssetFS{
		Asset:     Asset,
		AssetDir:  AssetDir,
		AssetInfo: AssetInfo,
	}
	http.Handle("/", http.FileServer(&fs))
	http.HandleFunc("/getAppName", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, *ProcessName)
	})
	http.HandleFunc("/getAll", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, fmt.Sprintf("%v", infoContain))
	})
	http.HandleFunc("/getMaxValue", func(w http.ResponseWriter, r *http.Request) {
		s, _ := ioutil.ReadAll(r.Body)
		var req map[string]interface{}
		err := json.Unmarshal(s, &req)
		log.Println("=====>", string(s))
		if err == nil && req["startTime"] != nil && req["endTime"] != nil && len(infoContain) > 2 {
			//log.Println("====type", fmt.Sprintf("%T", req["startTime"]))
			var startStamp, endStamp int64
			startStamp = int64(req["startTime"].(float64))
			endStamp = int64(req["endTime"].(float64))
			interval := infoContain[1].Stamps - infoContain[0].Stamps
			startPos := (startStamp - infoContain[0].Stamps) / interval
			endPos := (endStamp - infoContain[0].Stamps) / interval

			if int(startPos) > len(infoContain)-1 {
				startPos = int64(len(infoContain)) - 1
			}

			if int(endPos) > len(infoContain)-1 {
				endPos = int64(len(infoContain)) - 1
			}

			for index := startPos; index > 0; index-- {
				startPos = index
				if infoContain[index].Stamps < startStamp {
					startStamp = index
					break
				}
			}
			for index := endPos; index < int64(len(infoContain)); index++ {
				endPos = index
				if infoContain[index].Stamps > endStamp {
					endPos = index
					break
				}
			}
			log.Println("startStamp, contain[0].stamp, startPos, endPos ", startStamp, infoContain[0].Stamps, startPos, endPos)
			if startPos < 0 || startPos > endPos {
				startPos = 0
			}
			if endPos < 0 {
				endPos = 0
			}
			var maxcpu, maxmem, maxthread float64
			for _, info := range infoContain[startPos:endPos] {
				//log.Println("info ", info)
				if startStamp < info.Stamps && info.Stamps < endStamp {
					if maxcpu < info.FsCpu {
						maxcpu = info.FsCpu
					}
					if maxmem < info.FsMem {
						maxmem = info.FsMem
					}
					if maxthread < info.FsThread {
						maxthread = info.FsThread
					}
				}
			}
			fmt.Fprintf(w, fmt.Sprintf(`{"maxcpu":%v,"maxmem":%v,"maxthread":%v}`, maxcpu, maxmem, maxthread))

		}
	})
	http.HandleFunc("/getinfo", func(w http.ResponseWriter, r *http.Request) {
		var responseInfo map[string]interface{}
		responseInfo = make(map[string]interface{})
		var datas []interface{}

		s, _ := ioutil.ReadAll(r.Body)
		var req map[string]interface{}
		err := json.Unmarshal(s, &req)
		if err == nil && req["startTime"] != nil && req["endTime"] != nil && len(req["startTime"].(string)) > 0 && len(req["endTime"].(string)) > 0 {
			startStamp := transTime(req["startTime"].(string))
			endStamp := transTime(req["endTime"].(string))

			interval := infoContain[1].Stamps - infoContain[0].Stamps
			startPos := (startStamp - infoContain[0].Stamps) / interval
			endPos := (endStamp - infoContain[0].Stamps) / interval

			if int(startPos) > len(infoContain)-1 {
				startPos = int64(len(infoContain)) - 1
			}

			if int(endPos) > len(infoContain)-1 {
				endPos = int64(len(infoContain)) - 1
			}

			for index := startPos; index > 0; index-- {
				startPos = index
				if infoContain[index].Stamps < startStamp {
					startStamp = index
					break
				}
			}
			for index := endPos; index < int64(len(infoContain)); index++ {
				endPos = index
				if infoContain[index].Stamps > endStamp {
					endPos = index
					break
				}
			}

			log.Println("start end time ", req["startTime"].(string), startStamp, req["endTime"].(string), endStamp)
			for _, info := range infoContain[startPos:endPos] {
				if startStamp < info.Stamps && info.Stamps < endStamp {
					datas = append(datas, info)
				}
			}

		} else {
			var start_pos int
			lens := len(infoContain)
			if lens > 100 {
				start_pos = lens - 100
			} else {
				start_pos = 0
			}
			for i := start_pos; i < lens; i++ {
				datas = append(datas, infoContain[i])
			}
		}
		responseInfo["data"] = datas
		responseInfo["result"] = "success"
		datasStr, err := json.Marshal(responseInfo)
		if err != nil {
			fmt.Fprintf(w, "{\"result\":\"failed\"}")
		} else {
			log.Println("===>", string(datasStr))
			fmt.Fprintf(w, string(datasStr))
		}
	})
	http.ListenAndServe(httpserver, nil)
}
