import { useEffect, useState, type ReactNode } from "react";
import { getLang, tr, type Lang } from "@/lib/i18n";

type PartnerLogoProps = { name: string; children: ReactNode };

function PartnerLogo({ name, children }: PartnerLogoProps) {
  return (
    <div className="plogo" data-partner={name} role="img" aria-label={name}>
      <svg width="32" height="32" viewBox="0 0 32 32" fill="currentColor" aria-hidden="true" focusable="false">
        {children}
      </svg>
      <span aria-hidden="true">{name}</span>
    </div>
  );
}

const PARTNERS = [
  <PartnerLogo key="coinmarketcap" name="CoinMarketCap">
    <path fillRule="evenodd" d="M16 2a14 14 0 1 0 0 28 14 14 0 0 0 0-28Zm0 3.2c5.96 0 10.8 4.84 10.8 10.8S21.96 26.8 16 26.8 5.2 21.96 5.2 16 10.04 5.2 16 5.2Z" />
    <path d="m8.2 18.2 4.2-7.2 3.4 4.8 3.5-4.9 4.5 8h-4l-1.3-2.5-2.7 3.7-3.5-4.8-1.5 2.9h-2.6Z" />
  </PartnerLogo>,
  <PartnerLogo key="tokeninsight" name="TokenInsight">
    <path d="m16 2 12 7v14l-12 7-12-7V9l12-7Zm0 4L8 11v10l8 5 8-5V11l-8-5Z" />
    <path d="M10 10h12v4h-4v9h-4v-9h-4v-4Z" />
  </PartnerLogo>,
  <PartnerLogo key="coindesk" name="CoinDesk">
    {[210, 240, 270, 300, 330, 30, 60, 90, 120, 150].map((angle) => (
      <circle key={angle} cx={16 + 10 * Math.cos(angle * Math.PI / 180)} cy={16 + 10 * Math.sin(angle * Math.PI / 180)} r="2.35" />
    ))}
  </PartnerLogo>,
  <PartnerLogo key="chainalysis" name="Chainalysis">
    <path fillRule="evenodd" d="M16 2a14 14 0 1 0 0 28 14 14 0 0 0 0-28Zm0 3.4a10.6 10.6 0 1 1 0 21.2 10.6 10.6 0 0 1 0-21.2Z" />
    <path d="m7 18 8-11 3.5 4.6-8 11L7 18Zm7.5 3.4 8-11 2.5 3.3-8 11-2.5-3.3Z" />
    <path d="m11.5 10.5 9 4.8-2.5 3.5-9-4.8 2.5-3.5Z" />
  </PartnerLogo>,
  <PartnerLogo key="sumsub" name="sumsub">
    <path d="M5 5h8v5h6V5h8v8h-5v6h5v8h-8v-5h-6v5H5v-8h5v-6H5V5Zm8 8v6h6v-6h-6Z" />
  </PartnerLogo>,
  <PartnerLogo key="fireblocks" name="Fireblocks">
    <path fillRule="evenodd" d="M4 3h24v26H4V3Zm3 3v20h18V6H7Z" />
    <path d="m16 8 8 15H8l8-15Z" />
  </PartnerLogo>,
];

export function HomePartners() {
  const [lang, setLang] = useState<Lang>(() => getLang());
  useEffect(() => {
    const update = () => setLang(getLang());
    window.addEventListener("lb-lang", update);
    window.addEventListener("storage", update);
    return () => {
      window.removeEventListener("lb-lang", update);
      window.removeEventListener("storage", update);
    };
  }, []);
  const title = tr("合作伙伴", lang);

  return (
    <section className="wrap center" aria-labelledby="home-partners-title">
      <h2 className="st" id="home-partners-title">{title}</h2>
      <div className="partners mq">
        <div className="mq-track">
          {[0, 1].map((copy) => (
            <div className="mq-group" key={copy} aria-hidden={copy === 1 ? true : undefined}>
              {PARTNERS}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
