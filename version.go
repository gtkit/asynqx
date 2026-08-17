package asynqx

// Version 是本包的当前版本号，与 git 附注标签保持一致。
//
// 发版走 make release-patch / make release-minor：脚本会原地自增下面这行的版本号、
// 提交并据此打标签，因此这一行的形状——Version 常量赋值为带 v 前缀的三段版本号——
// 不能改动，本注释内也不得出现版本号字面量（脚本取文件里第一个匹配到的版本号）。
const Version = "v0.1.0"
