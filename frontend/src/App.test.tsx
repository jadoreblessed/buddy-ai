/// <reference lib="es2024.promise" />
import '@testing-library/jest-dom/vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import App from './App'

type Step = {
  id: string
  title: string
  detail: string
  actionType: 'none' | 'request_access'
  status: 'done' | 'current' | 'pending' | 'locked'
  resource?: string
}
type Request = {
  id: string
  internId: string
  planId: string
  stepId: string
  resource: string
  status: 'pending' | 'approved'
  createdAt: string
  approvedAt?: string
}
type Plan = {
  planId: string
  intern: string
  position: 'backend' | 'frontend' | 'analyst'
  role: string
  team: string
  progress: number
  steps: Step[]
  request: Request | null
  mentor: { id: string; name: string; channel: string }
}

type Reply = Response | Error | Promise<Response>
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status,
  headers: { 'Content-Type': 'application/json' },
})
const basePlan: Plan = {
  planId: 'onboarding-backend-v1',
  intern: 'Маша Ковалёва',
  position: 'backend',
  role: 'Backend-разработчик',
  team: 'Команда платформы',
  progress: 1,
  steps: [
    { id: 'intro', title: 'Знакомство с платформой', detail: 'Узнать владельцев сервисов.', actionType: 'none', status: 'done' },
    { id: 'access', title: 'Доступ к тестовому окружению', detail: 'Запросить доступ к стенду.', actionType: 'request_access', status: 'current', resource: 'Тестовое окружение' },
    { id: 'task', title: 'Запуск API локально', detail: 'Поднять сервис.', actionType: 'none', status: 'locked' },
  ],
  request: null,
  mentor: { id: 'mentor-1', name: 'Алексей Морозов', channel: 'Демо-мессенджер' },
}
const backendMaterials = [
  { id: 'backend-guide', title: 'Памятка backend', content: 'Сначала настрой локальный сервис.' },
  { id: 'assigned-plan:backend-v1', title: 'Назначенный маршрут', content: 'Твой план на сегодня.' },
]
function deferred<T>() {
  const { promise, resolve } = Promise.withResolvers<T>()
  return { promise, resolve }
}

function mockApi(overrides: Record<string, Reply[]> = {}) {
  const replies: Record<string, Reply[]> = {
    '/api/plan': [json(basePlan)],
    '/api/materials': [json({ materials: backendMaterials })],
    ...overrides,
  }
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const path = new URL(String(input), 'http://localhost').pathname
    const reply = replies[path]?.shift()
    if (!reply) throw new Error(`Unexpected ${init?.method ?? 'GET'} ${path}`)
    return reply instanceof Error ? Promise.reject(reply) : Promise.resolve(reply)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

beforeEach(() => {
  vi.stubGlobal('scrollTo', vi.fn())
  HTMLDialogElement.prototype.showModal = function () { this.setAttribute('open', '') }
  HTMLDialogElement.prototype.close = function () { this.removeAttribute('open') }
})

it('shows plan loading, handles an HTML server error safely, and retries the assigned plan', async () => {
  const firstPlan = deferred<Response>()
  const fetchMock = mockApi({ '/api/plan': [firstPlan.promise, json(basePlan)] })
  const user = userEvent.setup()
  render(<App />)

  expect(screen.getByRole('status')).toHaveTextContent('Загружаем назначенный план')
  firstPlan.resolve(new Response('<html>Gateway exploded</html>', { status: 502 }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить план (502)')
  expect(screen.queryByText(/Gateway exploded/)).not.toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: /Повторить/ }))
  expect(await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })).toBeInTheDocument()
  expect(screen.getByText(/Твой маршрут на позиции backend/)).toHaveTextContent('Команда платформы')
  expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '1')
  expect(fetchMock.mock.calls.filter(([path]) => path === '/api/plan')).toHaveLength(2)
})

it('confirms access explicitly, displays its status, and reflects mentor approval in the plan', async () => {
  const pendingRequest: Request = {
    id: 'REQ-42', internId: 'intern-1', planId: basePlan.planId,
    stepId: 'access', resource: 'Тестовое окружение', status: 'pending', createdAt: '2026-11-14T10:00:00Z',
  }
  const pendingPlan: Plan = {
    ...basePlan, request: pendingRequest,
    steps: basePlan.steps.map((step): Step => step.id === 'access' ? { ...step, status: 'pending' } : step),
  }
  const approvedPlan: Plan = {
    ...basePlan, progress: 2, request: { ...pendingRequest, status: 'approved', approvedAt: '2026-11-14T10:05:00Z' },
    steps: basePlan.steps.map((step): Step => ({ ...step, status: step.id === 'task' ? 'current' : 'done' })),
  }
  const fetchMock = mockApi({
    '/api/plan': [json(basePlan), json(pendingPlan), json(approvedPlan)],
    '/api/requests': [json(pendingRequest)],
    '/api/requests/REQ-42/approve': [json({ ...pendingRequest, status: 'approved' })],
  })
  const user = userEvent.setup()
  render(<App />)
  await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })

  await user.click(screen.getByRole('button', { name: /Моя заявка/ }))
  expect(screen.getByRole('heading', { name: 'Заявки пока нет' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: /К плану/ }))
  await user.click(screen.getByRole('button', { name: /Запросить доступ/ }))
  expect(within(screen.getByRole('dialog')).getByText(/Тестовое окружение/)).toBeInTheDocument()
  expect(fetchMock.mock.calls.some(([path]) => path === '/api/requests')).toBe(false)
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отмена' }))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: /Запросить доступ/ }))
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: /Создать заявку/ }))
  await screen.findByRole('button', { name: /Заявка REQ-42 · посмотреть статус/ })
  expect(fetchMock.mock.calls.find(([path]) => path === '/api/requests')?.[1]).toMatchObject({
    method: 'POST', headers: { 'X-Demo-User': 'intern-1' },
  })

  await user.click(screen.getByRole('button', { name: /Заявка REQ-42 · посмотреть статус/ }))
  expect(screen.getByRole('heading', { name: 'Заявка REQ-42' })).toBeInTheDocument()
  expect(screen.getByText('Ожидает согласования', { selector: '.request-badge' })).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: /Демо: согласовать заявку/ }))
  expect(await screen.findByText('Согласована')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /Демо: согласовать заявку/ })).not.toBeInTheDocument()
  expect(fetchMock.mock.calls.find(([path]) => path === '/api/requests/REQ-42/approve')?.[1]).toMatchObject({
    method: 'POST', headers: { 'X-Demo-User': 'mentor-1' },
  })
  await user.click(screen.getByRole('button', { name: /План адаптации/ }))
  expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '2')
  expect(screen.getByText(/Сейчас: Запуск API локально/)).toBeInTheDocument()
})

it('keeps the plan available and reports a failed access action', async () => {
  mockApi({ '/api/requests': [json({ error: 'unavailable' }, 503)] })
  const user = userEvent.setup()
  render(<App />)
  await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })
  await user.click(screen.getByRole('button', { name: /Запросить доступ/ }))
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: /Создать заявку/ }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Действие не выполнено')
  expect(screen.getByRole('button', { name: /Запросить доступ/ })).toBeEnabled()
})

it('retries a failed buddy answer, then opens an available inline citation in materials', async () => {
  const pendingAnswer = deferred<Response>()
  const fetchMock = mockApi({
    '/api/buddy/answer': [json({ error: 'Помощник временно недоступен' }, 503), pendingAnswer.promise],
  })
  const user = userEvent.setup()
  render(<App />)
  await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })
  await user.click(screen.getByRole('button', { name: /Спросить AI-buddy/ }))
  await user.type(screen.getByRole('textbox', { name: 'Твой вопрос' }), 'Как запустить сервис?')
  await user.click(screen.getByRole('button', { name: /Отправить/ }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Помощник временно недоступен')
  expect(screen.getByText('Как запустить сервис?')).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: /Повторить вопрос/ }))
  expect(screen.getByRole('status')).toHaveTextContent('AI-buddy готовит ответ')
  expect(screen.getByRole('textbox', { name: 'Твой вопрос' })).toBeDisabled()
  pendingAnswer.resolve(json({
    answer: 'Открой **памятку** и запусти `go run` [backend-guide].\nИсточники:\n[backend-guide]',
    sources: [{ id: 'backend-guide', title: 'Памятка backend', excerpt: 'Сначала настрой локальный сервис.' }],
    model: 'demo-model',
  }))
  expect(await screen.findByText('памятку')).toBeInTheDocument()
  expect(screen.getByText('go run')).toBeInTheDocument()
  const citation = screen.getByRole('button', { name: 'Открыть источник: Памятка backend' })
  await waitFor(() => expect(citation).toBeEnabled())
  expect(screen.queryByText('Источники:')).not.toBeInTheDocument()
  const buddyCalls = fetchMock.mock.calls.filter(([path]) => path === '/api/buddy/answer')
  expect(buddyCalls).toHaveLength(2)
  expect(buddyCalls[1]?.[1]).toMatchObject({ method: 'POST', body: JSON.stringify({ question: 'Как запустить сервис?' }) })

  await user.click(citation)
  expect(screen.getByRole('heading', { name: 'Материалы.' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Памятка backend' })).toBeInTheDocument()
  expect(screen.getByText('Сначала настрой локальный сервис.')).toBeInTheDocument()
})

it('keeps unavailable buddy citations disabled and shows uncited sources separately', async () => {
  mockApi({
    '/api/buddy/answer': [json({
      answer: 'Прочитай доступные документы.',
      sources: [{ id: 'outside-role', title: 'Чужой документ', excerpt: 'Не доступен этому плану' }],
    })],
  })
  const user = userEvent.setup()
  render(<App />)
  await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })
  await user.click(screen.getByRole('button', { name: /Спросить AI-buddy/ }))
  await user.type(screen.getByRole('textbox', { name: 'Твой вопрос' }), 'Что читать?')
  await user.click(screen.getByRole('button', { name: /Отправить/ }))
  expect(await screen.findByText('Прочитай доступные документы.')).toBeInTheDocument()
  expect(screen.getByText('Материалы по теме')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Открыть источник: Чужой документ' })).toBeDisabled()
})

it('shows only assigned-role materials and separates public links from instructions', async () => {
  const frontendPlan: Plan = {
    ...basePlan, planId: 'onboarding-frontend-v1', position: 'frontend', role: 'Frontend-разработчик',
    team: 'Команда интерфейсов',
  }
  mockApi({
    '/api/plan': [json(frontendPlan)],
    '/api/materials': [json({ materials: [
      { id: 'frontend-guide', title: 'UI-инструкция', content: 'Проверь навигацию с клавиатуры.' },
      { id: 'frontend-review', title: 'Чеклист ревью', content: 'Сверь экран с макетом.' },
      { id: 'public', title: 'Открытая документация', content: 'Внешняя справка.', url: 'https://example.org/docs' },
    ] })],
  })
  const user = userEvent.setup()
  render(<App />)
  await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })
  await user.click(screen.getByRole('button', { name: /^▤ Материалы$/ }))
  expect(await screen.findByRole('heading', { name: 'UI-инструкция' })).toBeInTheDocument()
  expect(screen.getByText('Проверь навигацию с клавиатуры.')).toBeInTheDocument()
  expect(screen.getByText('Материалы / Frontend-разработчик')).toBeInTheDocument()
  expect(screen.queryByText('Памятка backend')).not.toBeInTheDocument()
  await user.click(within(screen.getByRole('navigation', { name: 'Документы демо-маршрута' })).getByRole('button', { name: 'Чеклист ревью' }))
  expect(screen.getByText('Сверь экран с макетом.')).toBeInTheDocument()
  expect(screen.queryByText('Проверь навигацию с клавиатуры.')).not.toBeInTheDocument()
  const link = screen.getByRole('link', { name: /Открытая документация — внешний сайт/ })
  expect(link).toHaveAttribute('href', 'https://example.org/docs')
  expect(link).toHaveAttribute('target', '_blank')
  expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  expect(within(screen.getByRole('navigation', { name: 'Документы демо-маршрута' })).queryByText('Открытая документация')).not.toBeInTheDocument()
})

it('shows materials loading and retries after the materials endpoint fails', async () => {
  const pendingMaterials = deferred<Response>()
  const fetchMock = mockApi({ '/api/materials': [pendingMaterials.promise, json({ materials: backendMaterials })] })
  const user = userEvent.setup()
  render(<App />)
  await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })
  await user.click(screen.getByRole('button', { name: /^▤ Материалы$/ }))
  expect(screen.getByRole('status')).toHaveTextContent('Загружаем материалы')
  pendingMaterials.resolve(json({ error: 'unavailable' }, 503))
  expect(await screen.findByRole('alert')).toHaveTextContent('Не удалось загрузить материалы (503)')
  await user.click(screen.getByRole('button', { name: /Повторить/ }))
  expect(await screen.findByRole('heading', { name: 'Памятка backend' })).toBeInTheDocument()
  expect(fetchMock.mock.calls.filter(([path]) => path === '/api/materials')).toHaveLength(2)
})

it('handles an empty route and explains that no access request or instructions exist', async () => {
  mockApi({
    '/api/plan': [json({ ...basePlan, progress: 0, steps: [] })],
    '/api/materials': [json({ materials: [] })],
  })
  const user = userEvent.setup()
  render(<App />)
  expect(await screen.findByText('Все шаги плана выполнены.')).toBeInTheDocument()
  expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuemax', '0')
  await user.click(screen.getByRole('button', { name: /Моя заявка/ }))
  expect(screen.getByText('В этом плане пока нет шага для заявки.')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: /^▤ Материалы$/ }))
  expect(await screen.findByText('Для твоего маршрута пока нет инструкций.')).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: 'Открытые источники из кейса' })).not.toBeInTheDocument()
})

it('confirms a one-day absence, shows the request ID, deduplicates a repeat and approves it in the demo', async () => {
  const date = new Date(Date.now() + 86_400_000).toISOString().slice(0, 10)
  const request = { id: 'ABS-7', internId: 'intern-1', date, status: 'pending', createdAt: new Date().toISOString() }
  const fetchMock = mockApi({
    '/api/absences': [json({ requests: [] }), json({ request }, 201), json({ request }, 200), json({ requests: [{ ...request, status: 'approved' }] })],
    '/api/absences/ABS-7/approve': [json({ request: { ...request, status: 'approved', approvedAt: new Date().toISOString() } })],
  })
  const user = userEvent.setup()
  render(<App />)
  await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })
  await user.click(screen.getByRole('button', { name: /Отгул на день/ }))
  const dateInput = screen.getByLabelText('День отгула')
  fireEvent.change(dateInput, { target: { value: date } })
  await waitFor(() => expect(screen.getByRole('button', { name: 'Проверить заявку' })).toBeEnabled())
  await user.click(screen.getByRole('button', { name: 'Проверить заявку' }))
  expect(within(screen.getByRole('dialog')).getByText(date)).toBeInTheDocument()
  expect(fetchMock.mock.calls.filter(([path, init]) => path === '/api/absences' && init?.method === 'POST')).toHaveLength(0)
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отмена' }))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Проверить заявку' }))
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Создать заявку' }))
  expect(await screen.findByText('Заявка ABS-7: ожидает согласования.')).toBeInTheDocument()
  const createCall = fetchMock.mock.calls.find(([path, init]) => path === '/api/absences' && init?.method === 'POST')
  expect(createCall?.[1]).toMatchObject({
    method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Demo-User': 'intern-1' },
    body: JSON.stringify({ date }),
  })
  await user.click(screen.getByRole('button', { name: 'Проверить заявку' }))
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Создать заявку' }))
  await waitFor(() => expect(fetchMock.mock.calls.filter(([path, init]) => path === '/api/absences' && init?.method === 'POST')).toHaveLength(2))
  expect(screen.getAllByText('Заявка ABS-7')).toHaveLength(1)
  await user.click(screen.getByRole('button', { name: 'Демо: согласовать как наставник' }))
  expect(await screen.findByText('Заявка ABS-7: согласована.')).toBeInTheDocument()
  expect(screen.getByText('Согласована', { selector: '.request-badge' })).toBeInTheDocument()
  expect(fetchMock.mock.calls.find(([path]) => path === '/api/absences/ABS-7/approve')?.[1]).toMatchObject({
    method: 'POST', headers: { 'X-Demo-User': 'mentor-1' },
  })
  await user.click(screen.getByRole('button', { name: /План адаптации/ }))
  await user.click(screen.getByRole('button', { name: /Отгул на день/ }))
  expect(await screen.findByText('Согласована', { selector: '.request-badge' })).toBeInTheDocument()
})

it('reports absence service errors and rejected creation without claiming success', async () => {
  const date = new Date(Date.now() + 86_400_000).toISOString().slice(0, 10)
  const fetchMock = mockApi({
    '/api/absences': [
      json({ error: 'Сервис отсутствий недоступен' }, 503),
      json({ requests: [] }),
      json({ error: 'Демо-роль не разрешена' }, 403),
      json({ requests: [] }),
    ],
  })
  const user = userEvent.setup()
  render(<App />)
  await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })
  await user.click(screen.getByRole('button', { name: /Отгул на день/ }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Сервис отсутствий недоступен')
  expect(screen.getByRole('button', { name: 'Проверить заявку' })).toBeDisabled()
  await user.click(screen.getByRole('button', { name: 'Повторить загрузку' }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Проверить заявку' })).toBeEnabled())
  fireEvent.change(screen.getByLabelText('День отгула'), { target: { value: date } })
  await user.click(screen.getByRole('button', { name: 'Проверить заявку' }))
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Создать заявку' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Демо-роль не разрешена')
  expect(screen.queryByText(/Заявка ABS-/)).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Проверить заявку' })).toBeDisabled()
  await user.click(screen.getByRole('button', { name: 'Повторить загрузку' }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Проверить заявку' })).toBeEnabled())
  expect(fetchMock.mock.calls.filter(([path]) => path === '/api/absences')).toHaveLength(4)
})

it('uses the assigned mentor, confirms the edited draft, handles rejection, then shows the delivery receipt', async () => {
  const mentor = { id: 'mentor-2', name: 'Дина Орлова', channel: 'Демо-чат' }
  const text = 'Здравствуйте! Как начать первую задачу?'
  const message = { id: 'MSG-19', internId: 'intern-1', mentorId: mentor.id, text, status: 'sent', createdAt: new Date().toISOString() }
  const fetchMock = mockApi({
    '/api/plan': [json({ ...basePlan, mentor })],
    '/api/mentor/messages': [
      json({ mentor, messages: [] }),
      json({ error: 'Нет доступа к получателю' }, 403),
      json({ mentor, messages: [] }),
      json({ message }, 201),
      json({ mentor, messages: [message] }),
    ],
  })
  const user = userEvent.setup()
  render(<App />)
  await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })
  await user.click(screen.getByRole('button', { name: /Написать наставнику/ }))
  expect(screen.getByText('Дина Орлова')).toBeInTheDocument()
  expect(screen.queryByText('Алексей Морозов')).not.toBeInTheDocument()
  const draft = screen.getByRole('textbox', { name: 'Сообщение наставнику' })
  await waitFor(() => expect(screen.getByRole('button', { name: 'Проверить сообщение' })).toBeEnabled())
  await user.clear(draft)
  await user.type(draft, text)
  expect(fetchMock.mock.calls.filter(([path, init]) => path === '/api/mentor/messages' && init?.method === 'POST')).toHaveLength(0)
  await user.click(screen.getByRole('button', { name: 'Проверить сообщение' }))
  const dialog = screen.getByRole('dialog')
  expect(within(dialog).getByText('Дина Орлова')).toBeInTheDocument()
  expect(within(dialog).getByText(text)).toBeInTheDocument()
  await user.click(within(dialog).getByRole('button', { name: 'Отмена' }))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Проверить сообщение' }))
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отправить наставнику' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Нет доступа к получателю')
  expect(draft).toHaveValue(text)
  expect(screen.queryByText('Сообщение MSG-19: отправлено в демо-мессенджер.')).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Повторить загрузку' }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Проверить сообщение' })).toBeEnabled())
  await user.click(screen.getByRole('button', { name: 'Проверить сообщение' }))
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отправить наставнику' }))
  expect(await screen.findByText('Сообщение MSG-19: отправлено в демо-мессенджер.')).toBeInTheDocument()
  const sent = fetchMock.mock.calls.filter(([path, init]) => path === '/api/mentor/messages' && init?.method === 'POST')
  expect(sent).toHaveLength(2)
  expect(sent[1]?.[1]).toMatchObject({
    method: 'POST', headers: { 'X-Demo-User': 'intern-1', 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
  })
  expect(draft).toHaveValue('')
  await user.click(screen.getByRole('button', { name: /План адаптации/ }))
  await user.click(screen.getByRole('button', { name: /Написать наставнику/ }))
  expect(await screen.findByText('Сообщение MSG-19 · Отправлено')).toBeInTheDocument()
})

it('does not present a receipt for a mismatched mentor and routes only a suggested buddy action', async () => {
  const mentor = basePlan.mentor
  const fetchMock = mockApi({
    '/api/mentor/messages': [
      json({ mentor, messages: [] }),
      json({ message: { id: 'MSG-wrong', internId: 'intern-1', mentorId: 'someone-else', text: 'Вопрос', status: 'sent' } }, 201),
      json({ mentor, messages: [] }),
    ],
    '/api/buddy/answer': [
      json({ answer: 'Напиши наставнику.', sources: [] }),
      json({ answer: `Твой наставник — ${mentor.name}.`, sources: [], suggestedAction: 'message_mentor' }),
    ],
  })
  const user = userEvent.setup()
  render(<App />)
  await screen.findByRole('heading', { name: 'Доступ к тестовому окружению' })
  await user.click(screen.getByRole('button', { name: /Спросить AI-buddy/ }))
  await user.type(screen.getByRole('textbox', { name: 'Твой вопрос' }), 'Кому написать?')
  await user.click(screen.getByRole('button', { name: /Отправить/ }))
  expect(await screen.findByText('Напиши наставнику.')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /Написать наставнику ↗/ })).not.toBeInTheDocument()
  await user.type(screen.getByRole('textbox', { name: 'Твой вопрос' }), 'Кто мой наставник?')
  await user.click(screen.getByRole('button', { name: /Отправить/ }))
  const cta = await screen.findByRole('button', { name: 'Написать наставнику ↗' })
  await user.click(cta)
  await waitFor(() => expect(screen.getByRole('button', { name: 'Проверить сообщение' })).toBeEnabled())
  const draft = screen.getByRole('textbox', { name: 'Сообщение наставнику' })
  await user.clear(draft)
  await user.type(draft, 'Вопрос')
  await user.click(screen.getByRole('button', { name: 'Проверить сообщение' }))
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Отправить наставнику' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Сервер не подтвердил доставку')
  expect(draft).toHaveValue('Вопрос')
  expect(screen.queryByText(/Сообщение MSG-wrong: отправлено/)).not.toBeInTheDocument()
  expect(fetchMock.mock.calls.filter(([path, init]) => path === '/api/mentor/messages' && init?.method === 'POST')).toHaveLength(1)
})
