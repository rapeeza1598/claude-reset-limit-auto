[English](../README.md) | **ภาษาไทย**

# claude-reset-limit-auto

[![CI](https://github.com/rapeeza1598/claude-reset-limit-auto/actions/workflows/ci.yml/badge.svg)](https://github.com/rapeeza1598/claude-reset-limit-auto/actions/workflows/ci.yml)

Docker container ที่ยิง prompt สั้นๆ ไปหา Claude (ผ่าน Claude Code CLI) ตามเวลาที่กำหนดทุกวัน เพื่อให้ usage limit แบบ rolling 5 ชั่วโมงของ subscription (Pro/Max) รีเซ็ตตรงตามเวลาที่คาดเดาได้ แทนที่จะขึ้นอยู่กับว่าเราบังเอิญเริ่มคุยกับ Claude ตอนไหน

**เวลาที่ยิง (Asia/Bangkok):** 06:00, 11:00, 16:00, 21:00 — ห่างกันครั้งละ 5 ชั่วโมง ยกเว้นช่วงข้ามคืน 21:00→06:00 ซึ่งห่างกัน 9 ชั่วโมง

ก่อนยิงทุกครั้ง จะเช็คก่อนว่า "ช่วงชั่วโมงนี้" เคยยิงไปแล้วหรือยัง (ผ่าน state file) ถ้ายิงไปแล้วจะข้าม ไม่ยิงซ้ำ

## หลักการทำงาน (สำคัญ อ่านก่อนใช้)

- ใช้ **`CLAUDE_CODE_OAUTH_TOKEN`** (token อายุ 1 ปี ที่ผูกกับ subscription Pro/Max ของคุณ) — **ไม่ใช่** `ANTHROPIC_API_KEY`
- เหตุผล: `ANTHROPIC_API_KEY` คือบัญชี Console แบบจ่ายตาม token แยกกันคนละก้อนกับ limit 5 ชั่วโมงที่เห็นใน Claude Code/claude.ai ถ้าใช้ API key จะยิงไปคนละบัญชี ไม่ช่วยรีเซ็ต limit ของ subscription เลย
- ห้ามตั้งค่า `ANTHROPIC_API_KEY` ไว้ที่ไหนในโปรเจกต์นี้เด็ดขาด เพราะ Claude Code CLI จะเลือกใช้ `ANTHROPIC_API_KEY` ก่อน `CLAUDE_CODE_OAUTH_TOKEN` เสมอ (auth precedence) ถ้ามีทั้งคู่จะสลับไปยิงบัญชีผิดแบบเงียบๆ โดยไม่มี error ใดๆ
- Container รันด้วย non-root user (`appuser`) — ใช้ [`supercronic`](https://github.com/aptible/supercronic) แทน system cron เพราะ supercronic ไม่ต้องการ root (รันเป็น foreground process ธรรมดา ไม่มีการ drop privilege แบบ cron ดั้งเดิมที่ต้องเขียน `/etc/cron.d`) และยังสืบทอด environment variable ของ container ได้ตรงๆ ไม่ต้องมี workaround ใดๆ
- `curl`/`openssl`/`libcurl4` และ dependency สายรับรอง TLS ทั้งหมดที่ใช้ตอนโหลด Claude CLI ถูกแยกไปอยู่ใน build stage ต่างหาก (`installer`) ไม่ได้ติดตั้งอยู่ใน image ที่รันจริง — ลด attack surface / จำนวน CVE ของ image สุดท้ายลง

## ข้อกำหนดเบื้องต้น (Prerequisites)

- Docker + Docker Compose
- มี Claude Pro หรือ Max subscription และเคย `claude login` บนเครื่องที่จะรัน `claude setup-token` แล้ว (ไม่จำเป็นต้องเป็นเครื่องเดียวกับที่รัน container)

## วิธีติดตั้ง (Setup)

### 1. สร้าง long-lived token

รันคำสั่งนี้บนเครื่อง Mac ที่ login Claude Code ไว้แล้ว (เป็น interactive command จะเปิด browser ให้ authorize):

```bash
claude setup-token
```

จะได้ token พิมพ์ออกมาในเทอร์มินัล (ไม่มีการบันทึกไฟล์ให้อัตโนมัติ — ต้อง copy เก็บเอง) token นี้ใช้ได้ 1 ปี

### 2. ตั้งค่า environment

```bash
cp .env.example .env
```

เปิด `.env` แล้ววาง token ที่ได้ลงไปในบรรทัด:

```
CLAUDE_CODE_OAUTH_TOKEN=<วาง token ที่ copy มาตรงนี้>
```

**อย่าเพิ่มบรรทัด `ANTHROPIC_API_KEY=` ลงในไฟล์นี้** (ดูเหตุผลด้านบน)

### 3. build และรัน container

```bash
docker compose build
docker compose up -d
```

Container จะรันค้างไว้ (`restart: unless-stopped`) แล้ว cron ข้างในจะยิง `claude -p "hi"` ให้เองตามเวลา 06:00 / 11:00 / 16:00 / 21:00 (Asia/Bangkok) ทุกวัน

## ตรวจสอบว่าติดตั้งถูกต้อง (Verification)

ทำตามลำดับนี้หลัง build เสร็จครั้งแรก:

```bash
# 1. เช็คว่า claude binary อยู่ใน PATH จริง
docker compose exec claude-reset which claude

# 2. เช็คว่า token เข้ามาถึง environment ของ container
docker compose exec claude-reset printenv CLAUDE_CODE_OAUTH_TOKEN

# 3. รัน reset ด้วยมือดูก่อนว่าทำงานได้จริง
docker compose exec claude-reset /app/reset
# ควรเห็น JSON log บรรทัดหนึ่ง เช่น {"level":"INFO","msg":"ping ok","slot":"...","duration_ms":...}

# 4. รันซ้ำทันที ต้อง "skip" ไม่ยิงซ้ำ
docker compose exec claude-reset /app/reset
# ควรเห็น {"level":"INFO","msg":"skip","slot":"...","reason":"already pinged"}

# 5. เช็ค log ที่บันทึกไว้ถาวร (persist ข้าม restart เพราะอยู่บน volume)
cat data/reset.log

# 6. เช็คว่ารันเป็น non-root จริง
docker compose exec claude-reset whoami
# ควรได้ appuser ไม่ใช่ root
```

ถ้าขั้นตอนที่ 3 error เรื่อง auth (เช่น token หมดอายุ/ผิด) — state file จะไม่ถูกเขียน ทำให้ครั้งถัดไปสามารถลองยิงใหม่ได้ ไม่ค้างสถานะ "สำเร็จ" ปลอมๆ

## แก้ไขตารางเวลา (schedule)

เวลาที่ยิงตั้งค่าผ่าน `RESET_HOURS` ใน `.env` (ไม่ได้ hardcode ไว้ใน image) — container จะ generate cron schedule จากค่านี้ทุกครั้งที่เริ่มทำงาน

```
RESET_HOURS=6,11,16,21
```

แก้ค่านี้ (ใส่ชั่วโมงคั่นด้วย comma, 0-23, เวลาไทย) แล้ว restart container — **ไม่ต้อง build ใหม่**:

```bash
docker compose restart
```

ตรวจสอบว่าค่าใหม่ถูกนำไปใช้จริง:

```bash
docker compose exec claude-reset cat /app/crontab
```

## Log และ state file

ทั้งสองไฟล์อยู่บน volume `./data` (mount จาก host) เพื่อให้อยู่รอดข้าม container restart/recreate:

| ไฟล์             | ใช้ทำอะไร                                                                                 |
| ---------------- | ----------------------------------------------------------------------------------------- |
| `data/last_slot` | จำว่า "ช่วงชั่วโมงล่าสุด" (รูปแบบ `YYYY-MM-DD_HH`) ที่ยิงสำเร็จคือช่วงไหน — กันยิงซ้ำ     |
| `data/reset.log` | log แบบ JSON บรรทัดต่อบรรทัด (JSON Lines) ทุกครั้งที่ script รัน ไม่ว่าจะยิงจริงหรือ skip |

ดู log แบบ real-time จาก container ได้ด้วย:

```bash
docker compose logs -f
```

## คำสั่งที่ใช้บ่อย

```bash
docker compose ps            # เช็คว่า container รันอยู่/ไม่ crash
docker compose logs -f        # ดู log สด
docker compose down           # หยุดและลบ container (data/ ยังอยู่)
docker compose up -d --build  # build ใหม่แล้วรันใหม่ (เช่นหลังแก้โค้ด)
```

## แก้ปัญหาที่พบบ่อย (Troubleshooting)

- **`which claude` ไม่เจอ binary** — install script ของ Claude Code อาจเปลี่ยนตำแหน่งติดตั้งในเวอร์ชันใหม่กว่านี้ ให้แก้บรรทัด `ENV PATH="/home/appuser/.local/bin:${PATH}"` ใน `Dockerfile` ให้ตรงกับตำแหน่งจริง แล้ว build ใหม่
- **cron (supercronic) ยิงแต่ log ไม่มี `CLAUDE_CODE_OAUTH_TOKEN`** — ไม่ควรเกิดขึ้น เพราะ supercronic รันเป็น process ธรรมดา (ไม่ privilege-drop แบบ cron ดั้งเดิม) job ที่มันสั่งจะสืบทอด environment ของตัว container โดยตรงอยู่แล้ว ถ้าเจอปัญหานี้ให้เช็คว่า `.env` ถูกโหลดจริงด้วย `docker compose config`
- **เวลาที่ยิงดูเลื่อนไปจากที่ตั้งไว้ (offset 7 ชั่วโมง)** — timezone ไม่ถูกต้อง เช็คด้วย `docker compose exec claude-reset date` ควรตรงกับเวลาไทยปัจจุบัน
- **อยากทดสอบว่า cron ยิงจริงโดยไม่ต้องรอถึงเวลา** — แก้ `RESET_HOURS` ใน `.env` ชั่วคราวให้ตรงกับชั่วโมงถัดไป, `docker compose restart` (ไม่ต้อง build ใหม่), รอดู `docker compose logs -f`, แล้วอย่าลืมแก้กลับเป็น `6,11,16,21` ทีหลัง

## สิ่งที่ตั้งใจไม่ทำ (scope ที่ตัดออก)

- ไม่มีการต่ออายุ/หมุนเวียน token อัตโนมัติ (token อายุ 1 ปี — ต้องรัน `claude setup-token` ใหม่เองก่อนหมดอายุ)
- ไม่มี log rotation (log วันละ 4 บรรทัด JSON เล็กๆ กว่าจะเป็นไฟล์ใหญ่ใช้เวลาเป็นปี)
- ไม่มีระบบแจ้งเตือนเมื่อยิงไม่สำเร็จ — ต้องเข้ามาดู `docker compose logs` หรือ `data/reset.log` เอง
- การเช็คก่อนยิงเช็คแค่ "ยิงไปแล้วหรือยังในชั่วโมงนี้" (จาก state file ของตัวเอง) ไม่ได้เช็คสถานะ rate limit จริงจาก Claude — ถ้าใช้ Claude ตามปกติในช่วงนั้นอยู่แล้ว การยิงตามตารางอาจไม่มีผลอะไรเพิ่ม (เพราะ window เปิดอยู่แล้วจากการใช้งานจริง) แต่ก็ไม่เสียหายอะไรมากไปกว่าใช้ quota เล็กน้อย
