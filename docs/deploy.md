# 定位

deploy: 

    - 规定一些部署（编译构建、打包、发布）的流程
    - makefile 的写法命名 —— 对应上面的各种功能
    - changelog 的写法和命名 —— 对应 releaseNote 和 每次的提交上
    - 配置的环境隔离：暂时这里先决定只用 `dev` 和 `local` 两套环境。
    - 服务部署的几种方式：暂时这里决定只用 docker compose(一般只用) / k8s
    - 和github怎么协作进行 CI & 写ci.yaml
