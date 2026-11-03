# 邮件 / 短信 / KYC 接入现状

## 邮件（Resend）
- account 服务读取 `RESEND_API_KEY`、`RESEND_FROM`（可选 `RESEND_API_URL`）。两者都设置时通过 Resend `POST /emails` 发送验证码与通知邮件；未设置时使用开发 mock 发送器（不真实发信）。
- API Key 从不写入日志。`RESEND_FROM` 的域名必须在 Resend 后台完成验证。

## 短信
- 暂未接入短信服务商，`SmsSender` 仍为 mock，不会真实发送。
- **注意**：前端手机号注册流程要求短信验证码。在接入短信前，生产环境手机号注册不可用，建议前端暂时隐藏手机号注册入口，仅开放邮箱注册。

## KYC
- 维持人工审核：申请仅能通过管理后台 `kyc` 审核接口（2FA + 审计）批准或驳回，代码中无自动批准路径。
