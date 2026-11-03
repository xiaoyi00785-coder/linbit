/** 本地 mock / 演示回退开关：仅当 NEXT_PUBLIC_ENABLE_MOCK=1（本地联调）时启用；生产默认关闭，网络错误如实报错。 */
export const MOCK_ENABLED = process.env.NEXT_PUBLIC_ENABLE_MOCK === "1";
export const NET_ERR = "网络异常，请稍后再试";
