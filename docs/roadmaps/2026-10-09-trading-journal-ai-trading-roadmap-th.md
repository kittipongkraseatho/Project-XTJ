# Trading Journal → Autonomous AI Trading

## Master Development Roadmap

ฉบับ rebaseline จาก `Trading_Journal_AI_Trading_Roadmap_TH.docx` วันที่ 9 ตุลาคม 2026

> เอกสารนี้เป็น product/architecture target roadmap ส่วนสถานะที่ทำแล้วจริงอยู่ใน [Implementation Audit](./2026-10-09-trading-journal-implementation-audit.md)

## 1. ขอบเขตและการตีความ

เอกสารต้นฉบับที่แนบกำหนดทิศทางจาก Trading Journal ไปสู่ Autonomous AI Trading โดยมี architecture principles และ safety gates เป็นข้อกำหนดของระบบปลายทาง

คำขอในรอบนี้คือการ rebaseline เอกสารและ audit สิ่งที่ทำแล้วเท่านั้น ไม่ใช่การประกาศว่า phase อนาคต shipped และไม่ใช่การเปิดให้ AI ส่งคำสั่งซื้อขายทันที

สถานะ legacy Phase 0–10 ใน repository ยังคงบันทึกไว้เป็น platform capabilities ที่ส่งมอบแล้ว แต่ไม่ถูกนับเป็นการส่งมอบ R2–R5 หากยังไม่มี behavior intelligence, strategy validation, paper trading, risk boundary และ execution gates ตาม roadmap นี้

## 2. Product Vision

เป้าหมายคือ Trading Intelligence Platform ที่สร้างวงจรข้อมูลต่อเนื่อง:

`Human Trading → Analysis → Strategy → AI → Paper Trading → Live Trading → Feedback → Strategy Improvement`

North Star Flow:

`Trading Data / MT5 History → Trading Journal → Analytics → Trading Behavior Profile → Strategy Builder + Versioning → AI Strategy Intelligence → Market Intelligence / Regime → Backtest + OOS + Walk Forward → Paper Trading → AI Signal → Semi-Automatic Trading → Risk Engine → MetaApi Execution → Customer MT5 → AI Trade Journal + Continuous Improvement`

## 3. Product & Pricing Baseline

| Plan | ราคา | บัญชี | Auto Sync | จุดเด่น |
|---|---:|---:|---|---|
| Free | ฿0 | 1 | ไม่ | Basic Dashboard / จำกัดการใช้งาน |
| Basic | ฿99 | 1 | ไม่ | MT5 HTML/CSV Import + Analytics พื้นฐาน |
| Starter | ฿199 | 1 | ใช่ | MetaApi Auto Sync + Journal + Reports |
| Pro | ฿399 | 3 | ใช่ | Multi-account + Advanced Analytics |
| AI Add-on | แยก | ตามสิทธิ์ | ตามระดับ | AI Analysis / Strategy / Trading |

หลักการ: Subscription และ Entitlement ต้องแยกกัน เพื่อให้เพิ่ม AI Add-on หรือ plan ใหม่ได้โดยไม่ผูก business logic ไว้กับชื่อ plan แบบตายตัว

## 4. Architecture Principles

- Multi-tenant ตั้งแต่ต้น
- แยก Domain / Application / Infrastructure / Provider
- MT5 Order, Deal, Position และ Trade เป็นคนละ concept
- รองรับ Multiple Entry, Partial Close, Hedging, Commission และ Swap
- Strategy ต้องมี Version และ Trade ต้อง trace กลับไปยัง Strategy Version ที่ใช้
- เก็บ Market Snapshot และ Decision Record เพื่อรองรับ AI และ audit ในอนาคต
- MetaApi ต้องอยู่หลัง `TradingDataProvider` / `ExecutionProvider` abstraction
- Cloudinary ต้องอยู่หลัง `MediaService` abstraction
- AI ห้ามส่ง Order เข้า MetaApi โดยตรง
- Risk Engine เป็น Hard Boundary
- ทุก AI Trade ต้องสร้าง Audit/Decision Record
- Strategy ใหม่ต้องผ่าน Backtest → Out-of-Sample → Walk Forward → Paper Trading ก่อน Live

## 5. Technology Baseline

| ส่วน | Technology |
|---|---|
| Frontend | Nuxt 3, Vue 3, Vuetify 3, TypeScript, Pinia |
| Backend | Go REST API |
| Database | PostgreSQL |
| Queue / Cache | Redis + Background Workers |
| Image Storage | Cloudinary |
| MT5 Integration | MetaApi ผ่าน Provider Abstraction |
| Deployment | Docker + CI/CD + Environment Separation |

## 6. Release Roadmap

| Release | ชื่อ | เป้าหมาย | Scope | สถานะจาก audit |
|---|---|---|---|---|
| R0 | Foundation | วางรากฐานระบบ | Auth, Tenant, DB, API, Worker, Security, CI/CD | Partial* |
| R1 | Journal MVP | เก็บและวิเคราะห์ Trade ได้ | Account, MT5 Import, Journal, Cloudinary, Dashboard | Partial* |
| R2 | Trading Intelligence | เปลี่ยนข้อมูลเป็นความรู้ | Analytics, Behavior, Profile, Strategy | Implemented at capability level; release verification pending |
| R3 | AI Research | สร้างและทดสอบ AI/Strategy | AI Analysis, Regime, Backtest, OOS, Walk Forward | Partial |
| R4 | AI Trading Beta | AI ช่วยตัดสินใจแบบควบคุมได้ | Paper, Signal, Semi-Auto, Risk Engine | Not started |
| R5 | Autonomous Trading | AI เทรดจริงผ่าน MetaApi | Full Auto, Execution, AI Journal, Continuous Improvement | Not started |

`*` มี core implementation ใน repository แต่ release gate ยังไม่ sign-off จนกว่าจะผ่าน verification และแก้ blockers ตาม audit

## 7. Detailed Development Phases

### Phase 0 — Foundation & Architecture

Nuxt/Go/PostgreSQL/Redis, Auth + Tenant + RBAC, migrations/config/secret management, API conventions, worker framework, logging/health/CI/CD

**Definition of Done:** Build/Run/Test ได้ และโครงสร้างพร้อมต่อยอด

### Phase 1 — SaaS Core

Register/Login/Profile, Plan/Subscription/Entitlement, Usage Counters, Account management, Basic admin

**Definition of Done:** ผู้ใช้สมัครและจัดการ Account/Plan ได้

### Phase 2 — MT5 Import Engine

HTML/CSV parser, Order/Deal/Position/Trade model, normalize, deduplicate, import job + retry/error, commission/swap/partial close/hedging

**Definition of Done:** นำ history จริงเข้าระบบและได้ Trade model ที่ถูกต้อง

### Phase 3 — Journal + Cloudinary

Trade Journal, Tags, trade screenshots, Cloudinary upload/delete/secure delivery, image metadata และ plan-based image limits

**Definition of Done:** ผู้ใช้บันทึกเหตุผล/บทเรียนและแนบภาพได้

### Phase 4 — Analytics Engine

P/L, Win Rate, Profit Factor, Drawdown, Expectancy, equity curve, symbol/direction/session/time, Magic/EA/Strategy analysis, calendar และ risk dashboard

**Definition of Done:** ระบบตอบได้ว่าผู้ใช้เทรดอะไร ดี/แย่เมื่อไร และเพราะอะไร

### Phase 5 — Trading Behavior Intelligence

สถานะ follow-up 11 ตุลาคม 2026: ส่งมอบ deterministic behavior profile/detectors, tenant-scoped persistence, internal JWT API และหน้า `/behavior` แล้ว เหลือ database-backed/E2E release verification

Trading Profile, behavior detection, best/worst setup, risk behavior, streaks, overtrade และ revenge patterns

**Definition of Done:** สร้าง Profile เชิงพฤติกรรมจากข้อมูลจริง

### Phase 6 — Strategy Builder & Versioning

สถานะ follow-up 11 ตุลาคม 2026: ส่งมอบ Strategy, immutable Strategy Version, strict rule schema, trade traceability, audit, internal JWT API และหน้า `/strategies` แล้ว ไม่มี execution authority หรือ MetaApi order path

Structured strategy definition, conditions/entry/exit/risk, strategy versioning และ trade-to-strategy linkage

**Definition of Done:** เทคนิคถูกแปลงเป็น Rule ที่เครื่องอ่านได้

### Phase 7 — AI Strategy Intelligence

Pattern discovery, strategy review, optimization suggestions, candidate strategy generation และการแยก LLM/Quant

**Definition of Done:** AI วิเคราะห์และสร้าง Strategy Candidate ได้ แต่ยังไม่ Live

### Phase 8 — Market Intelligence

Price/OHLC/Indicators, ATR/Spread/Volatility, session, market regime และ external market data ในอนาคต

**Definition of Done:** AI เข้าใจสภาพตลาดปัจจุบันและเลือก Strategy ตาม Regime

### Phase 9 — Backtest & Validation

Historical backtest, OOS, Walk Forward, stress test และ performance/stability gates

**Definition of Done:** Strategy Candidate ต้องผ่าน validation ก่อนเข้าสู่ Paper

### Phase 10 — Paper Trading

Virtual execution, real-time market input, virtual P/L, paper performance และ paper risk limits

**Definition of Done:** AI เทรดจำลองในตลาดจริงก่อนใช้เงินจริง

### Phase 11 — AI Signal

Signal engine, confidence, reasoning, decision record และ signal history

**Definition of Done:** AI ส่ง Signal แต่ยังไม่ส่ง Order

### Phase 12 — Semi-Auto

User approval, execution preview, risk validation และ MetaApi test execution

**Definition of Done:** User อนุมัติทุก Order ก่อนส่ง MT5

### Phase 13 — Risk & Execution Engine

Hard risk limits, position sizing, daily loss/drawdown, max positions/lot, emergency stop และ execution state

**Definition of Done:** AI ไม่สามารถข้าม Risk Boundary ได้

### Phase 14 — Autonomous Live Trading

Full auto, MetaApi execution, open/modify/partial close/close, account-specific permissions และ fail-safe

**Definition of Done:** AI เทรดจริงได้ภายใต้สิทธิ์และ Risk ที่กำหนด

### Phase 15 — AI Trade Journal & Continuous Improvement

Auto AI journal, model/strategy version record, post-trade analysis, degradation detection และ candidate → validation loop

**Definition of Done:** ระบบเรียนรู้จากผลลัพธ์ แต่ไม่แก้ Live Strategy โดยพลการ

### Phase 16 — Scale / Multi-account / Global Intelligence

Multi-account AI, strategy per account/shared, AI Add-on, opt-in anonymized aggregate learning และ advanced admin/observability

**Definition of Done:** ขยายเป็น AI Trading Platform เชิงพาณิชย์

## 8. Core Data Model Target

### Identity & Billing

`users`, `tenants`, `memberships`, `audit_logs`, `plans`, `subscriptions`, `subscription_events`, `usage_counters`

### Trading

`trading_accounts`, `account_connections`, `orders`, `deals`, `positions`, `trades`

### Import/Sync

`imported_files`, `import_jobs`, `sync_cursors`, `sync_events`

### Journal & Analytics

`journal_entries`, `trade_tags`, `trade_images`, `daily_metrics`, `equity_snapshots`

### Future Intelligence

`strategies`, `strategy_versions`, `market_snapshots`, `decision_records`, `ai_runs`, `paper_orders`, `risk_events`

## 9. Critical End-to-End Flows

| Flow | Process |
|---|---|
| Manual Import | MT5 Export → Upload → Validate → Parse → Normalize → Deduplicate → Persist → Analytics |
| Auto Sync | MT5 → MetaApi → Sync Worker → Normalize → Upsert → Reconcile → Analytics |
| Journal | Trade → Journal → Tags → Cloudinary Image → Review → Analytics |
| Strategy Discovery | Trade Data + Journal + Market Snapshot → Behavior → Pattern → Strategy Candidate |
| AI Validation | Strategy Candidate → Backtest → OOS → Walk Forward → Paper Trading → Gate |
| Semi Auto | Market → AI Signal → User Approval → Risk Engine → MetaApi → MT5 |
| Full Auto | Market → AI Decision → Strategy Engine → Risk Engine → Execution Engine → MetaApi → MT5 |
| Continuous Improvement | Live Result → AI Journal → Performance Analysis → Degradation Detection → Candidate → Validation → Approval → New Version |

## 10. AI Trading Safety Gates

- AI ต้องไม่ส่งคำสั่งเข้า MetaApi โดยตรง
- ทุก Order ต้องผ่าน Risk Engine
- Strategy ใหม่ต้องผ่าน Backtest
- ต้องมี Out-of-Sample และ Walk Forward
- ต้องมี Paper Trading ก่อน Live
- ต้องมี User/Risk Gate ก่อนเปิด Live
- ต้องมี Emergency Stop
- SL/TP ควรถูกวางที่ฝั่ง MT5/Broker ไม่พึ่ง AI เพียงอย่างเดียว
- MetaApi/Provider ล่มต้องหยุด New Orders และแจ้งเตือน
- AI ห้ามเปลี่ยน Live Strategy โดยพลการ
- Strategy ใหม่ต้องสร้างเป็น Version ใหม่และผ่าน Validation

## 11. Recommended AI Architecture

`LLM Layer` — Journal understanding, explanation, review, natural language, strategy description

`Quant/ML Layer` — Pattern discovery, regime, probability, scoring, optimization

`Strategy Engine` — แปลง Strategy Version เป็น executable rules

`Signal Engine` — สร้าง Buy/Sell/No Trade พร้อม confidence

`Risk Engine` — Hard limits, sizing, drawdown, daily loss, exposure

`Execution Engine` — Order lifecycle และ state management

`MetaApi Adapter` — เชื่อมบัญชี MT5 ของลูกค้า

## 12. Suggested Development Order for AI Agent

1. Inspect repository, architecture, database, auth, deployment และ current features
2. Create/update ADR สำหรับ core boundaries
3. Implement database/domain model ก่อน complex UI
4. Implement one vertical slice end-to-end ก่อนขยาย module
5. Build MT5 parser/normalizer ด้วย real historical fixtures และ regression tests
6. Build Analytics จาก normalized Trade model
7. Build Journal และ Cloudinary integration
8. เพิ่ม Strategy และ Version model หลัง Analytics stable
9. เพิ่ม AI Research layer โดยแยกจาก live execution อย่างชัดเจน
10. Implement Backtest/OOS/Walk Forward และ Paper Trading gates
11. แยก Signal → Risk → Execution เป็น services/modules
12. Integrate MetaApi หลัง Paper/Semi-Auto ผ่าน validation
13. เปิด Full Auto ผ่าน feature flag และ permission เท่านั้น
14. เพิ่ม continuous improvement หลัง Live AI มี auditability ที่เสถียร

## 13. Release Gates

| Release | Minimum Gate |
|---|---|
| R1 Journal MVP | Import correctness, deduplication, P/L correctness, Cloudinary security, tenant isolation |
| R2 Intelligence | Analytics regression tests, behavior metrics correctness, strategy version traceability |
| R3 AI Research | No leakage, reproducible backtest, OOS/WFA validation, model/version traceability |
| R4 AI Trading Beta | Paper performance, risk enforcement, execution simulation, emergency stop |
| R5 Autonomous | MetaApi integration tests, fail-safe, permissions, broker-side SL/TP, audit trail |

## 14. Final Target Architecture

ระบบปลายทางมี 3 วงจรหลัก:

- **Human Trading Loop:** User → Trade → Journal → Analytics
- **Strategy Intelligence Loop:** Data → Behavior → Pattern → Strategy → Validation
- **Autonomous Loop:** Market → AI → Strategy → Risk → MetaApi → MT5 → Result → Journal → Improvement

## 15. Current Implementation Overlay

| Capability | Current evidence | Target interpretation |
|---|---|---|
| Foundation / SaaS core | Auth, tenant, RBAC, plans, subscriptions, accounts, workers, CI files | Core implementation exists; R0/R1 release sign-off still blocked |
| Import / Journal / Analytics | HTML/CSV parser, normalized trading tables, import jobs, journal, tags, images, analytics pages/API | R1 substantially implemented; R2 analytics foundation exists |
| Broker integration | Mock provider plus MetaApi REST Connect/FetchDeals/Ping | Historical sync only; not Execution Engine |
| Billing / Webhooks / Public API | Stripe state machine, outbound webhooks, scoped read/write API keys, idempotency | Cross-cutting platform enablers |
| AI | Per-trade analysis, weekly recap, Q&A chat with RAG/pgvector | AI platform capability; not AI Strategy Research completion |
| Behavior / Strategy / Market | Behavior/Strategy profile + versioning slice exists; market snapshot/regime remains absent | R3 gap |
| Validation / Paper / Signal | No Backtest, OOS, WFA, paper order, signal or decision-record loop | R3/R4 gap |
| Risk / Execution / Live | No hard Risk Engine or order execution path | R4/R5 not started |

## 16. Next Step

R2 behavior/strategy WBS (`2026-10-11-r2-trading-intelligence-plan.md`) ถูก implement แล้วเป็น vertical slice; เหลือ run migration/E2E บน PostgreSQL จริงก่อน release sign-off

ลำดับ product work ถัดไปคือ **Trading Behavior Intelligence → Strategy Builder & Versioning** ก่อนเริ่ม AI Strategy generation หรือการเชื่อม execution ใด ๆ
