(() => {
  'use strict';
  const instances = new Set();
  // 手动输入与日历使用相同校验，不能让2月30日等错误日期自动滚到下个月。
  function parse(value, hourly) {
    const match = String(value).match(hourly ? /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2})(?::00)?$/ : /^(\d{4})-(\d{2})-(\d{2})$/);
    if (!match) return;
    const [, year, month, day, hour = '0'] = match.map(Number);
    const date = new Date(year, month - 1, day, hour);
    if (date.getFullYear() === year && date.getMonth() === month - 1 && date.getDate() === day && hour >= 0 && hour <= 23) return date;
  }

  // 使用当前视口定位，窄屏和窗口缩放时日历不能越出左右边界。
  function positionCalendar(instance) {
    const calendar = instance.calendarContainer, anchor = instance.input.getBoundingClientRect();
    const width = calendar.offsetWidth, height = calendar.offsetHeight;
    const above = innerHeight - anchor.bottom < height + 8 && anchor.top >= height + 8;
    const left = Math.max(8, Math.min(anchor.left, document.documentElement.clientWidth - width - 8));
    const top = Math.max(8, Math.min(above ? anchor.top - height - 7 : anchor.bottom + 7, innerHeight - height - 8));
    calendar.style.left = `${scrollX + left}px`; calendar.style.top = `${scrollY + top}px`; calendar.style.right = 'auto';
  }

  function bind(input, language, t) {
    if (input._flatpickr) return;
    const hourly = input.dataset.datePicker === 'hour';
    let invalidText = input.value && !parse(input.value, hourly) ? input.value : '';
    const validate = () => input.setCustomValidity(input.value && !parse(input.value, hourly) ? t(hourly ? 'invalidStartHour' : 'invalidDate') : '');
    input.addEventListener('input', () => { invalidText = input.value && !parse(input.value, hourly) ? input.value : ''; validate(); });
    // Flatpickr默认会清空无效手输值；保留原文，让使用者看清错误并修正。
    input.addEventListener('blur', event => { validate(); if (input.validity.customError) event.stopImmediatePropagation(); }, true);
    input.addEventListener('keydown', event => {
      if (event.key === 'Enter' && !parse(input.value, hourly)) { validate(); event.preventDefault(); event.stopImmediatePropagation(); }
    }, true);
    const picker = window.flatpickr(input, {
      locale: window.flatpickr.l10ns[language] || window.flatpickr.l10ns.default,
      dateFormat: hourly ? 'Y-m-d H:00' : 'Y-m-d',
      enableTime: hourly, time_24hr: true, minuteIncrement: 60, defaultMinute: 0,
      disableMobile: true, allowInput: true, monthSelectorType: 'dropdown', animate: false, position: positionCalendar,
      parseDate: value => parse(value, hourly),
      onOpen: (_, __, instance) => {
        const date = parse(input.value, hourly);
        if (date) instance.setDate(date, false);
      },
      onChange: dates => {
        if (dates.length) invalidText = '';
        else if (invalidText) input.value = invalidText;
        validate();
      },
      onClose: () => { if (invalidText) input.value = invalidText; validate(); },
      onReady: (_, __, instance) => {
        const calendar = instance.calendarContainer;
        calendar.classList.add('tb-calendar');
        if (hourly) {
          calendar.classList.add('tb-calendar-hour');
          instance.hourElement.setAttribute('aria-label', t('hour'));
          const suffix = document.createElement('span'); suffix.className = 'tb-hour-suffix'; suffix.textContent = t('hour');
          instance.timeContainer.append(suffix);
        }
        const footer = document.createElement('div'); footer.className = 'tb-calendar-footer';
        const button = (label, fn, primary = false) => {
          const item = document.createElement('button'); item.type = 'button'; item.textContent = label;
          if (primary) item.className = 'tb-calendar-done';
          item.addEventListener('click', fn); footer.append(item);
        };
        button(t('today'), () => {
          const now = new Date(); now.setMinutes(0, 0, 0); instance.setDate(now, true);
          if (!hourly) instance.close();
        });
        if (!input.required) button(t('clear'), () => { invalidText = ''; instance.clear(); instance.close(); });
        if (hourly) button(t('done'), () => instance.close(), true);
        calendar.append(footer);
        if (invalidText) input.value = invalidText;
      }
    });
    if (!picker) return;
    instances.add(picker);
    input.form?.addEventListener('formrestored', () => {
      const date = parse(input.value, hourly);
      invalidText = input.value && !date ? input.value : '';
      if (date) picker.setDate(date, false);
      else if (!input.value) picker.clear(false);
      validate();
    });
    input.form?.addEventListener('reset', () => queueMicrotask(() => { invalidText = ''; picker.setDate(input.defaultValue || [], false); validate(); }));
    validate();
  }

  window.TBDatePicker = {
    bindAll(root, language, t) {
      for (const instance of instances) if (!instance.input.isConnected) { instance.destroy(); instances.delete(instance); }
      root.querySelectorAll('[data-date-picker]').forEach(input => bind(input, language, t));
    },
    destroyAll() { for (const instance of instances) instance.destroy(); instances.clear(); }
  };
})();
