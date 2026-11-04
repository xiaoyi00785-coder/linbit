export const metadata = { title: "服务不可用 / Not available" };

export default function Blocked() {
  return (
    <main style={{ minHeight: "70vh", display: "flex", alignItems: "center", justifyContent: "center", padding: 24, textAlign: "center" }}>
      <div style={{ maxWidth: 520 }}>
        <h1 style={{ fontSize: 24, marginBottom: 12 }}>您所在的国家或地区暂不支持访问</h1>
        <p style={{ opacity: 0.8, marginBottom: 24 }}>根据适用法律法规，Linbit 不向您所在的国家或地区（包括美国）提供服务。</p>
        <h2 style={{ fontSize: 18, marginBottom: 8 }}>Not available in your region</h2>
        <p style={{ opacity: 0.8 }}>Due to applicable laws and regulations, Linbit does not provide services in your country or region (including the United States).</p>
      </div>
    </main>
  );
}
