import { i18n } from "@lingui/core"
import { messages } from "@/locales/en/messages.po"

export function initI18n(locale = "en"): void {
  i18n.load(locale, messages)
  i18n.activate(locale)
}
export { i18n }
