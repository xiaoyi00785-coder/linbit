# 生产窗口 instrumentation（NOT RUN：无设备）

`ProductionDialogPixelCopyTest` 的 5 个 case（loginHelp/changePassword/antiPhishing/convertFinalReview/withdrawFinalReview）使用生产 `AuthFlow`/`SecurityHost`/`ConvertSubpage`/`AssetsHost` 及生产 `LbSheet`/`ModalBottomSheet` 的 `DialogWindowProvider.window`，随后在真实窗口上执行 `PixelCopy.request`。它不使用离屏 inline 代理窗口，不手工加错误文字，也不截取 Activity 根替代独立 Dialog。

```bash
export JAVA_HOME=/path/to/jdk17
export ANDROID_HOME=/path/to/android-sdk
./gradlew :app:compileDebugAndroidTestKotlin
./gradlew :app:connectedDebugAndroidTest \
  --tests 'click.cowa.linbit.instrumentation.ProductionDialogPixelCopyTest'
```

runner 启动空 `InstrumentationEvidenceApplication`，不引导生产 Hilt 应用或持久会话；只向 loopback `MockWebServer` 发送无效输入的登录 challenge、GET 安全/资产数据及演示 convert quote。未声明路由直接失败；password/anti-phishing/withdraw/convert accept/2FA 终结点及其他敏感写入 fail-closed，不真实开通、重置密码、绑定身份验证器或提交资金操作。窗口不存在或 PixelCopy 失败即测试失败，不 Use `Assume`、不降级到主 Activity。

五个 PNG/JSON 输出应在设备应用专属 `getExternalFilesDir(DIRECTORY_PICTURES)/production-evidence` 下；客户端外部目录没有真实 API host 或资产账户。仅可在没有真实凭据的测试设备/模拟器上执行。当前环境只做了编译验证；**未启动设备，未执行 instrumentation，未签视觉通过**。
