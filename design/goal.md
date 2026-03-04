# 目标

设计 rbg cli，实现以下功能：
1. 用户快速上手使用 rbg 部署模型服务

## 已完成阶段

1. 添加三种 plugin 接口
2. source 接口实现 huggingface 和 modelscope
3. storage 接口实现 pvc 类型
4. engine 接口实现 vllm 和 sglang
5. 模型下载和模型部署功能先只执行到 podTemplate 生成，暂不部署完整工作负载，仅将 template 打印出来，并标记 TODO
    1. 即暂时只完整实现 kubectl rbg llm config 命令，pull 和 run 只实现 template 生成，其他命令暂不实现

## 本次实施阶段

1. 

## 相关文档

1. 命令行设计 - /Users/wangdi/github/diw-zw/rbg/design/command.md
2. 插件接口及使用流程设计 - /Users/wangdi/github/diw-zw/rbg/design/design.md
