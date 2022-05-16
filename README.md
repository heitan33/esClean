##代码逻辑说明：
1、程序启动后立即获取当es磁盘的使用率,之后每五分钟调用linux命令获取当前es数据磁盘的磁盘使用率
2、如果es数据盘使用率超过stage-high所设定的值，执行删除不在白名单内的索引,并再次获取磁盘使用率,当磁盘使用率缩减到stage-low时,停止删除索引
3、通过es的接口获取当前存在的所有的index并按其数据量排序，每次删除存储日志占用最高的一个index
4、为防止其他进程占用磁盘空间过多,磁盘使用率达不到stage-low,每次删除任务会在持续30秒后退出，并等待下次定时任务

##注意事项
程序已做异常处理。host key的ip与本机ip不一致或者stage-high小于stage-low时程序不会执行

##配置文件说明
{
  "data": [
    {
      "host": "http://1.117.11.230:9200",		//host地址必须与本机ip一致
      "diskName": "/dev/sda1",					//es数据目录所在磁盘
      "stage-high": 12,
      "stage-low": 10,
      "index-white": "service,envoy,database"	//白名单索引的头部字符,用逗号分隔且不能有空格
    }
  ]
}


##配置文件举例如下(请根据实际情况修改host,diskName,stage的值):
'''''''''''''''''''''''''''''''''''''''''
{
  "data": [
    {
      "host": "http://1.117.11.230:9200",
      "diskName": "/dev/sda1",
      "stage-high": 12,
      "stage-low": 10,
      "index-white": "service,envoy,database"
    }
  ]
}
'''''''''''''''''''''''''''''''''''''''''
