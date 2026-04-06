import { defineConfig } from "vitepress";

const repo = "tamago0224/kuroshio-mta";
const repoName = process.env.GITHUB_REPOSITORY?.split("/")[1] ?? "kuroshio-mta";

export default defineConfig({
  title: "kuroshio-mta Docs",
  description: "kuroshio-mta の設定、RFC 対応、runbook、アーキテクチャ資料",
  base: process.env.GITHUB_ACTIONS ? `/${repoName}/` : "/",
  cleanUrls: true,
  lastUpdated: true,
  themeConfig: {
    logo: "/logo.svg",
    nav: [
      { text: "Guide", link: "/configuration" },
      { text: "Architecture", link: "/architecture/normalization_policy" },
      { text: "Runbooks", link: "/runbooks/admin_api" },
      { text: "GitHub", link: `https://github.com/${repo}` }
    ],
    search: {
      provider: "local"
    },
    sidebar: [
      {
        text: "Getting Started",
        items: [
          { text: "Home", link: "/" },
          { text: "Configuration", link: "/configuration" },
          { text: "Rate Limit", link: "/rate_limit" },
          { text: "Kafka Queue Mode", link: "/kafka_queue_mode" }
        ]
      },
      {
        text: "Architecture",
        items: [
          { text: "Normalization Policy", link: "/architecture/normalization_policy" },
          { text: "HA Reference", link: "/architecture/ha_reference" },
          { text: "Spec", link: "/spec" }
        ]
      },
      {
        text: "Runbooks",
        items: [
          { text: "Admin API", link: "/runbooks/admin_api" },
          { text: "DR Backup / Restore", link: "/runbooks/dr_backup_restore" },
          { text: "Load / Chaos", link: "/runbooks/load_chaos" },
          { text: "Reputation Ops", link: "/runbooks/reputation_ops" },
          { text: "SLO Backlog", link: "/runbooks/slo_backlog" },
          { text: "SLO Delivery", link: "/runbooks/slo_delivery" },
          { text: "SLO Retry", link: "/runbooks/slo_retry" }
        ]
      },
      {
        text: "Security",
        items: [
          { text: "Secrets and Supply Chain", link: "/security/secrets_and_supply_chain" }
        ]
      },
      {
        text: "RFC Coverage",
        items: [
          { text: "RFC 1870: SMTP SIZE", link: "/rfc_1870_gap" },
          { text: "RFC 3207: SMTP STARTTLS", link: "/rfc_3207_gap" },
          { text: "RFC 3464: DSN", link: "/rfc_3464_gap" },
          { text: "RFC 4954: SMTP AUTH", link: "/rfc_4954_gap" },
          { text: "RFC 6152: 8BITMIME", link: "/rfc_6152_gap" },
          { text: "RFC 6376: DKIM", link: "/rfc_6376_gap" },
          { text: "RFC 6409: Message Submission", link: "/rfc_6409_gap" },
          { text: "RFC 7208: SPF", link: "/rfc_7208_gap" },
          { text: "RFC 7489: DMARC", link: "/rfc_7489_gap" },
          { text: "RFC 7672: DANE for SMTP", link: "/rfc_7672_gap" },
          { text: "RFC 8461: MTA-STS", link: "/rfc_8461_gap" },
          { text: "RFC 8617: ARC", link: "/rfc_8617_gap" }
        ]
      }
    ],
    socialLinks: [{ icon: "github", link: `https://github.com/${repo}` }],
    footer: {
      message: "Built with VitePress and GitHub Pages",
      copyright: "kuroshio-mta"
    }
  },
  head: [["link", { rel: "icon", href: "/logo.svg" }]]
});
