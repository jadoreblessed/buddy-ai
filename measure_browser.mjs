#!/usr/bin/env node
// Run: cd frontend && npm ci && npx playwright install chromium
// Then from the repository root: node measure_browser.mjs --url http://127.0.0.1:5173 --samples 10
import fs from 'node:fs'
import path from 'node:path'
import { createRequire } from 'node:module'

const { chromium } = createRequire(new URL('./frontend/package.json', import.meta.url))('playwright')

function usage() {
  console.log('Usage: node measure_browser.mjs [--url URL] [--samples 1..50] [--output FILE]\nInstall: cd frontend && npm ci && npx playwright install chromium')
}

const args = new Map()
for (let index = 2; index < process.argv.length; index += 2) {
  if (process.argv[index] === '--help') {
    usage()
    process.exit(0)
  }
  if (!['--url', '--samples', '--output'].includes(process.argv[index]) || !process.argv[index + 1]) {
    usage()
    process.exit(2)
  }
  args.set(process.argv[index], process.argv[index + 1])
}
const url = args.get('--url') || process.env.BUDDY_URL || 'http://127.0.0.1:5173'
const samples = Number(args.get('--samples') ?? 10)
if (!Number.isInteger(samples) || samples < 1 || samples > 50) {
  throw new Error('--samples must be an integer from 1 to 50')
}
const output = args.get('--output') || 'validation/browser_metrics.json'

function percentile(values, fraction) {
  const ordered = [...values].sort((a, b) => a - b)
  const position = (ordered.length - 1) * fraction
  const lower = Math.floor(position)
  return ordered[lower] + (ordered[Math.ceil(position)] - ordered[lower]) * (position - lower)
}

function summary(values) {
  return {
    median_ms: Number(percentile(values, 0.5).toFixed(2)),
    p95_ms: Number(percentile(values, 0.95).toFixed(2)),
    raw_samples_ms: values.map(value => Number(value.toFixed(2))),
  }
}

// This executes in each new document, before application code. The timer starts at navigation
// start (performance.now() === 0), and ends after the visible CTA has had a paint opportunity.
function watchPlan() {
  const visible = selector => {
    const element = document.querySelector(selector)
    return element && element.getClientRects().length > 0 && getComputedStyle(element).visibility !== 'hidden'
  }
  let done = false
  const observer = new MutationObserver(() => {
    if (done || !visible('.step-current .action-button')) return
    done = true
    observer.disconnect()
    requestAnimationFrame(() => setTimeout(() => {
      window.__planLoadMs = performance.now()
    }, 0))
  })
  observer.observe(document, { childList: true, subtree: true, attributes: true })
}

// The capture listener timestamps the actual trusted click event, not the Playwright call.
// A post-mutation animation frame followed by a task measures the first paint opportunity
// for the requested visible state. The same click and predicate must complete each sample.
async function measureClick(page, target, selector, text) {
  await page.evaluate(({ selector, text, target }) => {
    const visible = () => {
      const element = [...document.querySelectorAll(selector)].find(node =>
        node.textContent.includes(text) && node.getClientRects().length > 0 &&
        getComputedStyle(node).visibility !== 'hidden')
      return Boolean(element)
    }
    if (visible()) throw new Error(`Target state already visible: ${selector} / ${text}`)
    window.__clickTiming = { started: null, elapsed: null, error: null }
    const state = window.__clickTiming
    const observer = new MutationObserver(() => {
      if (state.started === null || !visible() || state.elapsed !== null) return
      observer.disconnect()
      requestAnimationFrame(() => setTimeout(() => {
        state.elapsed = performance.now() - state.started
      }, 0))
    })
    const button = document.querySelector(target)
    if (!button || button.disabled || button.getClientRects().length === 0) {
      observer.disconnect()
      throw new Error(`Click target unavailable: ${target}`)
    }
    button.addEventListener('click', event => {
      if (!event.isTrusted) {
        state.error = 'Expected a trusted browser click'
        return
      }
      state.started = performance.now()
    }, { capture: true, once: true })
    observer.observe(document, { childList: true, subtree: true, attributes: true, characterData: true })
  }, { selector, text, target })
  await page.locator(target).click()
  await page.waitForFunction(() => Number.isFinite(window.__clickTiming?.elapsed) || window.__clickTiming?.error, null, { timeout: 15000 })
  const timing = await page.evaluate(() => window.__clickTiming)
  if (timing.error || timing.started === null || !Number.isFinite(timing.elapsed)) {
    throw new Error(timing.error || `Click did not render ${selector} / ${text}`)
  }
  return timing.elapsed
}

const browser = await chromium.launch({ headless: true })
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
  await page.addInitScript(watchPlan)
  const raw = {
    plan_navigation_to_visible_cta: [],
    cta_click_to_visible_confirmation: [],
    navigation_click_to_visible_buddy: [],
    submit_click_to_visible_greeting: [],
    suggestion_click_to_visible_mentor_answer: [],
  }
  for (let index = 0; index < samples; index++) {
    await page.goto(url, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => Number.isFinite(window.__planLoadMs), null, { timeout: 15000 })
    raw.plan_navigation_to_visible_cta.push(await page.evaluate(() => window.__planLoadMs))

    raw.cta_click_to_visible_confirmation.push(await measureClick(
      page, '.step-current .action-button', '[role="dialog"] h2', 'Запросить доступ?'))
    await page.getByRole('button', { name: 'Отмена' }).click()

    raw.navigation_click_to_visible_buddy.push(await measureClick(
      page, 'nav button:nth-child(2)', '.buddy-page h1', 'Спроси AI-buddy'))

    await page.getByRole('textbox', { name: 'Твой вопрос' }).fill('Привет')
    raw.submit_click_to_visible_greeting.push(await measureClick(
      page, '.buddy-form button[type="submit"]', '.buddy-answer', 'Привет, Маша!'))

    await page.goto(url, { waitUntil: 'domcontentloaded' })
    await page.getByRole('button', { name: /Спросить AI-buddy/ }).click()
    await page.getByRole('button', { name: 'Кто мой наставник?' }).waitFor({ state: 'visible' })
    raw.suggestion_click_to_visible_mentor_answer.push(await measureClick(
      page, '.buddy-suggestions button:nth-child(4)', '.buddy-answer', 'Твой назначенный наставник'))
  }

  const report = {
    url,
    generated_at: new Date().toISOString(),
    sample_count: samples,
    mode: 'Chromium headless, 1280x900 viewport; browser performance.now() trusted click to visible DOM mutation followed by requestAnimationFrame and post-frame task; navigation start to visible CTA post-frame task',
    caveat: 'Paint opportunity is an approximation, not a GPU presentation timestamp. Results depend on host hardware, load and browser mode; async AI/API responses are included in click metrics.',
    metrics: Object.fromEntries(Object.entries(raw).map(([name, values]) => [name, summary(values)])),
  }
  fs.mkdirSync(path.dirname(output), { recursive: true })
  fs.writeFileSync(output, JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report, null, 2))
} finally {
  await browser.close()
}
