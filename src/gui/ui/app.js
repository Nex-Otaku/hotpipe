// Логика окна hotpipe.
//
// Своего учёта здесь нет: туннели и их состояние берутся у сервера, правки
// уходят туда же. Окно лишь показывает и передаёт.

const invoke = (cmd, args) => window.__TAURI__.invoke(cmd, args);

/** Длительность словами: «2 ч 13 мин», «45 с». */
function human(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return s + ' с';
  const m = Math.floor(s / 60);
  if (m < 60) return m + ' мин';
  const h = Math.floor(m / 60);
  if (h < 24) return h + ' ч ' + (m % 60) + ' мин';
  return Math.floor(h / 24) + ' д ' + (h % 24) + ' ч';
}

/**
 * Разбирает привычную команду ssh в поля туннеля:
 *   ssh -o ServerAliveInterval=1 -p '2222' -f -N -L 5001:127.0.0.1:3306 user@host
 * Понимает кавычки, склеенные флаги (-fN), -p, -l, -i, -L и ssh://user@host:port.
 * Возвращает поля или строку с объяснением, что не так.
 */
function parseSSH(line) {
  const tok = [];
  const re = /'([^']*)'|"((?:\\.|[^"\\])*)"|(\S+)/g;
  let m;
  while ((m = re.exec(line)) !== null) tok.push(m[1] ?? (m[2] !== undefined ? m[2].replace(/\\(.)/g, '$1') : m[3]));
  if (tok.length && /(^|\/)ssh(\.exe)?$/.test(tok[0])) tok.shift();

  // Ключи ssh, принимающие значение.
  const withArg = 'BbcDEeFIiJLlmOoPpQRSWw';
  const out = {};
  let dest = null;
  const forwards = [];
  for (let i = 0; i < tok.length; i++) {
    const t = tok[i];
    if (t === '--') { dest = tok[i + 1] || null; break; }
    if (t.startsWith('-') && t.length > 1) {
      for (let j = 1; j < t.length; j++) {
        const f = t[j];
        if (!withArg.includes(f)) continue;
        const val = j + 1 < t.length ? t.slice(j + 1) : tok[++i];
        if (f === 'p') out.port = parseInt(val, 10);
        if (f === 'l') out.user = val;
        if (f === 'i') out.identity_file = val;
        if (f === 'L') forwards.push(val);
        if (f === 'o') {
          const [k, v] = val.split('=');
          if (/^port$/i.test(k)) out.port = parseInt(v, 10);
          if (/^user$/i.test(k)) out.user = v;
          if (/^identityfile$/i.test(k)) out.identity_file = v;
        }
        break;
      }
      continue;
    }
    if (dest === null) dest = t;
  }
  if (!dest) return 'не найден адрес сервера (user@host)';
  if (!forwards.length) return 'нет перенаправления -L';

  let d = dest.replace(/^ssh:\/\//, '');
  const at = d.lastIndexOf('@');
  if (at >= 0) { out.user = d.slice(0, at); d = d.slice(at + 1); }
  const pm = /^(\[[^\]]+\]|[^:]+):(\d+)$/.exec(d);
  if (pm && dest.startsWith('ssh://')) { d = pm[1]; out.port = parseInt(pm[2], 10); }
  out.host = d.replace(/^\[|\]$/g, '');

  // [адрес:]порт:удалённый_адрес:удалённый_порт; адреса IPv6 — в скобках.
  const parts = forwards[0].match(/\[[^\]]*\]|[^:]+/g) || [];
  const strip = (s) => s.replace(/^\[|\]$/g, '');
  if (parts.length === 4) { out.local_host = strip(parts[0]); parts.shift(); }
  if (parts.length !== 3) return 'перенаправление -L ' + forwards[0] + ' не разобрано';
  out.local_port = parseInt(parts[0], 10);
  out.remote_host = strip(parts[1]);
  out.remote_port = parseInt(parts[2], 10);
  if (forwards.length > 1) out.note = 'взято первое из ' + forwards.length + ' перенаправлений';
  return out;
}

function hotpipe() {
  return {
    status: null,
    message: '',
    messageIsError: false,
    // Действие окна (включение, выключение, перезапуск сервера) и его подпись.
    acting: false,
    workLabel: '',
    // К какому положению идёт переключатель, пока действие не кончилось.
    target: null,
    // Номер действия: ответ опроса, запрошенный до него, отбрасывается — иначе
    // на миг вернулось бы прежнее состояние (урок wsldrive, Р-052 там).
    epoch: 0,
    // Разница часов окна и сервера; длительности считаются по часам сервера.
    skew: 0,
    focused: true,
    timer: null,

    form: null,
    formTitle: '',
    formError: '',
    formBusy: false,
    pasted: '',
    pasteNote: '',

    settings: null,
    settingsError: '',
    distros: [],

    confirmDelete: null,
    confirmTimer: null,
    openOutput: {},

    logTab: 'server',
    logText: '',
    logPath: '',

    get working() {
      return this.acting || !!(this.status && this.status.busy);
    },

    get tunnels() {
      return (this.status && this.status.server && this.status.server.tunnels) || [];
    },

    start() {
      this.focused = document.hasFocus();
      window.addEventListener('focus', () => { this.focused = true; this.schedule(); });
      window.addEventListener('blur', () => { this.focused = false; this.schedule(); });
      this.refresh();
      this.schedule();
    },

    // При фокусе — раз в секунду, без фокуса — раз в пять: на окно не смотрят.
    schedule() {
      clearInterval(this.timer);
      this.timer = setInterval(() => this.refresh(), this.focused ? 1000 : 5000);
    },

    async refresh() {
      const epoch = this.epoch;
      try {
        const s = await invoke('get_status');
        if (epoch !== this.epoch) return;
        if (s.server) this.skew = Date.now() - Date.parse(s.server.now);
        this.status = s;
        if (s.notice && !this.message) this.warn(s.notice);
      } catch (e) {
        this.fail('состояние не получено: ' + e);
      }
    },

    fail(text) { this.message = text; this.messageIsError = true; },
    warn(text) { this.message = text; this.messageIsError = false; },

    // --- сервер ---

    switchOn() {
      if (this.target !== null) return this.target;
      return !!(this.status && this.status.config.server_enabled);
    },

    headline() {
      const s = this.status;
      if (!s) return 'Узнаю состояние…';
      if (!s.config.server_enabled) return 'Выключен — туннели не поддерживаются';
      if (!s.server) return 'Не отвечает';
      const ts = s.server.tunnels;
      const on = ts.filter((t) => t.enabled);
      const up = ts.filter((t) => t.state === 'up');
      if (!ts.length) return 'Работает, туннелей нет';
      let text = 'Работает · подняты ' + up.length + ' из ' + on.length;
      const bad = ts.filter((t) => t.state === 'waiting').length;
      if (bad) text += ' · со сбоем ' + bad;
      return text;
    },

    async act(label, target, cmd) {
      this.epoch++;
      this.acting = true;
      this.target = target;
      this.workLabel = label;
      this.message = '';
      try {
        await invoke(cmd);
      } catch (e) {
        this.fail(String(e));
      } finally {
        this.epoch++;
        await this.refresh();
        this.acting = false;
        this.target = null;
      }
    },

    toggleServer() {
      if (this.switchOn()) {
        this.act('Выключаю сервер…', false, 'server_off');
      } else {
        this.act('Включаю сервер… (WSL может стартовать секунд десять)', true, 'server_on');
      }
    },

    restartServer() {
      this.settings = null;
      this.act('Перезапускаю сервер…', true, 'server_restart');
    },

    async setAutostart(on) {
      try {
        await invoke('set_autostart', { on });
      } catch (e) {
        this.fail('автозапуск не изменён: ' + e);
      }
      this.refresh();
    },

    // --- туннели ---

    async call(method, path, body) {
      try {
        const r = await invoke('api', { method, path, body: body ?? null });
        this.refresh();
        return r;
      } catch (e) {
        this.fail(String(e));
        this.refresh();
        throw e;
      }
    },

    stateLabel(t) {
      const now = Date.now() - this.skew;
      const since = now - Date.parse(t.since);
      switch (t.state) {
        case 'up': return 'работает ' + human(since);
        case 'connecting': return 'подключаюсь…';
        case 'waiting': {
          const left = t.retry_at ? Date.parse(t.retry_at) - now : 0;
          return left > 0 ? 'сбой, повтор через ' + human(left) : 'сбой, повторяю…';
        }
        default: return 'выключен';
      }
    },

    stateClass(t) {
      return {
        up: 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300',
        connecting: 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300',
        waiting: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-300',
      }[t.state] || 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300';
    },

    via(t) {
      return (t.user ? t.user + '@' : '') + t.host + (t.port !== 22 ? ':' + t.port : '');
    },

    openTunnel(t) {
      this.formError = '';
      this.pasted = '';
      this.pasteNote = '';
      this.formTitle = t ? t.name : '';
      this.form = t
        ? {
            id: t.id, name: t.name, enabled: t.enabled, host: t.host, port: t.port, user: t.user,
            local_host: t.local_host, local_port: t.local_port, remote_host: t.remote_host,
            remote_port: t.remote_port, identity_file: t.identity_file || '',
          }
        : {
            id: '', name: '', enabled: true, host: '', port: 22, user: '', local_host: 'localhost',
            local_port: null, remote_host: '127.0.0.1', remote_port: null, identity_file: '',
          };
    },

    parsePasted() {
      if (!this.pasted.trim()) { this.pasteNote = ''; return; }
      const r = parseSSH(this.pasted);
      if (typeof r === 'string') { this.pasteNote = r; return; }
      const note = r.note;
      delete r.note;
      Object.assign(this.form, r);
      if (!this.form.name) this.form.name = r.host + ':' + r.local_port;
      this.pasteNote = '✓ разобрано' + (note ? ' — ' + note : '');
    },

    async saveTunnel() {
      this.formBusy = true;
      this.formError = '';
      const { id, ...body } = this.form;
      for (const k of ['port', 'local_port', 'remote_port']) body[k] = Number(body[k]) || 0;
      try {
        if (id) await invoke('api', { method: 'PUT', path: '/api/tunnels/' + id, body });
        else await invoke('api', { method: 'POST', path: '/api/tunnels', body });
        this.form = null;
        this.refresh();
      } catch (e) {
        this.formError = String(e);
      } finally {
        this.formBusy = false;
      }
    },

    removeTunnel(t) {
      if (this.confirmDelete !== t.id) {
        this.confirmDelete = t.id;
        clearTimeout(this.confirmTimer);
        this.confirmTimer = setTimeout(() => { this.confirmDelete = null; }, 3000);
        return;
      }
      this.confirmDelete = null;
      this.call('DELETE', '/api/tunnels/' + t.id).catch(() => {});
    },

    // --- настройки ---

    async openSettings() {
      const s = this.status;
      if (!s) return;
      this.settingsError = '';
      const set = (s.server && s.server.settings) || {};
      this.settings = {
        distro: s.config.distro, api_port: s.config.api_port, server_binary: s.config.server_binary,
        server_alive_interval: set.server_alive_interval, server_alive_count_max: set.server_alive_count_max,
      };
      this.distros = await invoke('list_distros').catch(() => []);
    },

    async saveSettings() {
      const st = this.settings;
      const s = this.status;
      try {
        await invoke('save_settings', {
          cfg: { distro: st.distro, api_port: Number(st.api_port), server_binary: st.server_binary.trim(), server_enabled: true },
        });
        const cur = s && s.server && s.server.settings;
        if (cur && (cur.server_alive_interval !== Number(st.server_alive_interval) ||
                    cur.server_alive_count_max !== Number(st.server_alive_count_max))) {
          await invoke('api', {
            method: 'PUT', path: '/api/settings',
            body: { server_alive_interval: Number(st.server_alive_interval), server_alive_count_max: Number(st.server_alive_count_max) },
          });
        }
        this.settings = null;
        this.refresh();
      } catch (e) {
        this.settingsError = String(e);
      }
    },

    // --- журналы ---

    async loadLogs() {
      try {
        if (this.logTab === 'server') {
          const r = await invoke('api', { method: 'GET', path: '/api/log' });
          this.logText = r.log;
          this.logPath = r.path ? 'WSL: ' + r.path : '';
        } else {
          this.logText = await invoke('gui_log');
          this.logPath = 'hotpipe-gui.log рядом с программой';
        }
      } catch (e) {
        this.logText = 'не получен: ' + e;
      }
      this.$nextTick(() => { const el = this.$refs.log; if (el) el.scrollTop = el.scrollHeight; });
    },
  };
}

// Для проверки разбора в node: node -e "require('./app.js')"
if (typeof module !== 'undefined') module.exports = { parseSSH, human };
