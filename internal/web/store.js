"use strict";

const byId = (id) => document.getElementById(id);
const themeToggle = byId("theme-toggle");
// Тема относится к браузеру, а не к аккаунту. Локальное хранилище сохраняет выбор
// после обновления страницы, включая гостевой режим.
const themeKey = "linia-theme";

function setTheme(dark) {
  document.documentElement.dataset.theme = dark ? "dark" : "light";
  themeToggle.setAttribute("aria-checked", String(dark));
  themeToggle.title = dark ? "Включить светлую тему" : "Включить тёмную тему";
}

try {
  setTheme(localStorage.getItem(themeKey) === "dark");
} catch {
  setTheme(false);
}

themeToggle.addEventListener("click", () => {
  const dark = document.documentElement.dataset.theme !== "dark";
  setTheme(dark);
  try {
    localStorage.setItem(themeKey, dark ? "dark" : "light");
  } catch {
    // The toggle still works for this page if storage is unavailable.
  }
});

const money = new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB" });
const rubles = (kopecks) => money.format(kopecks / 100);
const route = (prefix, id) => prefix + "/" + encodeURIComponent(id);
const pageSize = 12;
const offsets = { catalog: 0, orders: 0, managers: 0 };
const navigation = Array.from(document.querySelectorAll("[data-view]"));
const authDialog = byId("auth-dialog");
const authForm = byId("auth-form");
const productDialog = byId("product-dialog");
const productForm = byId("product-form");
const photoForm = byId("photo-form");
const managerDialog = byId("manager-dialog");
const managerForm = byId("manager-form");
let user = null;
let view = "catalog";
let registering = false;
let productID = null;
let catalogFilters = {};
let revision = 0;
let managerRoleID;

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function button(label, className, action) {
  const node = element("button", className, label);
  node.type = "button";
  node.addEventListener("click", () => run(node, action));
  return node;
}

function notice(message, error = false, target = byId("notice")) {
  target.textContent = message;
  target.classList.toggle("error", error);
  target.hidden = !message;
}

async function run(control, action, target) {
  if (control.disabled) return;
  control.disabled = true;
  try {
    await action();
  } catch (error) {
    notice(error.message, true, target);
  } finally {
    control.disabled = false;
  }
}

// Единая функция запросов добавляет защитный заголовок и переводит ошибки API.
// Сессионную cookie прикладывает браузер, её значение скрипту недоступно.
async function api(path, method = "GET", data) {
  const headers = { Accept: "application/json" };
  if (method !== "GET") {
    const csrf = document.cookie.split("; ").find((cookie) => cookie.startsWith("clothing_csrf="));
    if (csrf) headers["X-CSRF-Token"] = csrf.slice("clothing_csrf=".length);
  }
  let body = data;
  if (data !== undefined && !(data instanceof FormData)) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(data);
  }
  let response;
  try {
    response = await fetch(path, { method, headers, body, credentials: "same-origin", cache: "no-store" });
  } catch {
    throw new Error("Связь с магазином прервалась. Проверьте, запущен ли сервер.");
  }
  if (response.status === 204) return null;
  const result = await response.json().catch(() => null);
  if (!response.ok) {
    const messages = {
      400: "Проверьте заполнение полей.",
      401: path === "/auth/login" ? "Проверьте почту и пароль." : "Войдите в аккаунт, чтобы продолжить.",
      403: "Действие недоступно. Если сессия истекла, обновите страницу и войдите снова.",
      404: "Запись не найдена.",
      409: "Действие недоступно для текущего состояния записи.",
      413: "Файл слишком большой.",
      429: "Слишком много попыток входа. Попробуйте позже.",
    };
    const detail = {
      "email is already registered": "Эта почта уже зарегистрирована.",
      "cart is empty": "Корзина пуста.",
      "order state conflict": "Этот переход статуса недоступен.",
      "product is referenced by order history": "Этот товар входит в историю заказов. Удалить его нельзя.",
      "operation conflicts with protected account or role data": "Действие затрагивает защищённые данные. Проверьте, нет ли у аккаунта истории заказов или совпадающей почты.",
    }[result?.error];
    throw Object.assign(new Error(detail || messages[response.status] || "Сервер не смог выполнить запрос."), { status: response.status });
  }
  return result;
}

function canEditCatalog() {
  return ["admin", "manager"].includes(user?.role.name);
}

// Кнопки показывают доступные роли действия. Фактические права проверяет сервер,
// поскольку скрытые элементы интерфейса можно обойти прямым запросом.
function renderSession() {
  const role = user?.role.name;
  byId("account").hidden = Boolean(user);
  byId("logout").hidden = !user;
  byId("new-product").hidden = !canEditCatalog();
  document.querySelector('[data-view="cart"]').hidden = role !== "client";
  const orders = document.querySelector('[data-view="orders"]');
  orders.hidden = !["client", "admin", "manager"].includes(role);
  orders.textContent = role === "client" ? "Мои заказы" : "Заказы";
  document.querySelector('[data-view="managers"]').hidden = role !== "admin";
  document.querySelector('[data-view="analytics"]').hidden = !canEditCatalog();
}

const analyticsStatuses = ["Принят", "В работе", "Собран", "Готов к выдаче", "Отменён"];

// Столбцы сравнивают текущие категории заказов, а не динамику по датам.
// Общий максимум сохраняет сопоставимый масштаб, числа остаются видимыми при нуле.
function renderAnalytics(summary) {
  byId("analytics-total").textContent = summary.total.toLocaleString("ru-RU");
  byId("analytics-cancelled").textContent = summary.cancelled.toLocaleString("ru-RU");
  byId("analytics-amount").textContent = rubles(summary.non_cancelled_amount);
  byId("analytics-empty").hidden = summary.total !== 0;
  const bars = byId("analytics-bars");
  bars.replaceChildren();
  const counts = analyticsStatuses.map((status) => summary.by_status[status]);
  const max = Math.max(1, ...counts);
  const ns = "http://www.w3.org/2000/svg";
  analyticsStatuses.forEach((status, i) => {
    const count = counts[i];
    const column = element("li", "analytics-column");
    const plot = document.createElementNS(ns, "svg");
    plot.setAttribute("viewBox", "0 0 60 140");
    plot.setAttribute("aria-hidden", "true");
    const bar = document.createElementNS(ns, "rect");
    const height = count / max * 140;
    bar.setAttribute("x", "8");
    bar.setAttribute("y", String(140 - height));
    bar.setAttribute("width", "44");
    bar.setAttribute("height", String(height));
    bar.setAttribute("rx", "4");
    if (i === 3) bar.setAttribute("class", "ready-bar");
    if (i === 4) bar.setAttribute("class", "cancelled-bar");
    plot.append(bar);
    column.append(element("strong", "", String(count)), plot, element("span", "", status));
    bars.append(column);
  });
}

async function getManagerRoleID() {
  if (!managerRoleID) {
    const roles = await api("/role");
    managerRoleID = roles.find((role) => role.name === "manager")?.id;
    if (!managerRoleID) throw new Error("Роль менеджера не найдена.");
  }
  return managerRoleID;
}

// Раздел получает список только роли manager. Проверку прав и удаление сессий
// выполняют существующие серверные маршруты управления пользователями.
function renderManagers(page) {
  const list = byId("manager-items");
  list.replaceChildren();
  updatePages("manager", page);
  if (!page.items?.length) list.append(element("p", "empty", "Менеджеров пока нет."));
  for (const manager of page.items || []) {
    const row = element("article", "row");
    const profile = element("div");
    profile.append(element("h3", "", manager.name), element("p", "muted", manager.email), element("p", "muted", manager.phone));
    row.append(profile, button("Удалить менеджера", "quiet danger", async () => {
      if (!window.confirm("Удалить аккаунт «" + manager.email + "»?")) return;
      await api(route("/user", manager.id), "DELETE");
      notice("Менеджер удалён.");
      await refresh();
    }));
    list.append(row);
  }
}

function updatePages(prefix, page) {
  byId(prefix + "-pages").hidden = page.total <= pageSize;
  byId(prefix + "-prev").disabled = page.offset === 0;
  byId(prefix + "-next").disabled = page.offset + pageSize >= page.total;
  byId(prefix + "-page").textContent = "Страница " + (Math.floor(page.offset / pageSize) + 1);
}

async function paged(path, key, filters = {}) {
  const request = () => api(path + "?" + new URLSearchParams({ ...filters, limit: pageSize, offset: offsets[key] }));
  let page = await request();
  if (page.offset > 0 && !page.items?.length) {
    offsets[key] = Math.max(0, Math.ceil(page.total / pageSize) - 1) * pageSize;
    page = await request();
  }
  return page;
}

// Запросы к разным разделам могут завершиться не по порядку. Номер запроса позволяет
// отбросить устаревший ответ после перехода в другой раздел или смены периода.
async function refresh() {
  const current = ++revision;
  if (view === "catalog") {
    const page = await paged("/product", "catalog", catalogFilters);
    if (current === revision) renderCatalog(page);
  } else if (view === "cart") {
    const cart = await api(route("/cart", user.id));
    if (current === revision) renderCart(cart.items || []);
  } else if (view === "managers") {
    const id = await getManagerRoleID();
    const page = await paged(route("/user/role", id), "managers");
    if (current === revision) renderManagers(page);
  } else if (view === "analytics") {
    byId("analytics-results").hidden = true;
    byId("analytics-loading").hidden = false;
    try {
      const params = new URLSearchParams({period: byId("analytics-period").value});
      const summary = await api("/order/analytics?" + params);
      if (current === revision) {
        renderAnalytics(summary);
        byId("analytics-results").hidden = false;
      }
    } finally {
      if (current === revision) byId("analytics-loading").hidden = true;
    }
  } else {
    const page = await paged("/order", "orders");
    if (current === revision) renderOrders(page);
  }
}

function show(name) {
  view = name;
  notice("");
  for (const item of navigation) {
    const active = item.dataset.view === view;
    byId(item.dataset.view).hidden = !active;
    if (active) item.setAttribute("aria-current", "page");
    else item.removeAttribute("aria-current");
  }
  return refresh();
}

function renderCatalog(page) {
  const products = byId("products");
  products.replaceChildren();
  byId("catalog-count").textContent = "Найдено товаров: " + page.total;
  updatePages("catalog", page);
  if (!page.items?.length) {
    const message = Object.keys(catalogFilters).length
      ? "По этим фильтрам ничего не найдено. Измените параметры или сбросьте их."
      : "Каталог пока пуст. Добавить товар может администратор или менеджер.";
    products.append(element("p", "empty", message));
  }
  for (const product of page.items || []) {
    const brands = byId("product-brands");
    if (product.brand && !Array.from(brands.options).some((option) => option.value === product.brand)) {
      const option = element("option");
      option.value = product.brand;
      brands.append(option);
    }
    const card = element("article", "card");
    const picture = element("div", "picture");
    const fallback = element("span", "", "Фото пока нет");
    const photo = element("img");
    photo.alt = product.name;
    photo.loading = "lazy";
    photo.addEventListener("load", () => {
      photo.classList.add("loaded");
      fallback.hidden = true;
    });
    photo.addEventListener("error", () => { photo.hidden = true; });
    photo.src = route("/web/product-photo", product.id);
    picture.append(fallback, photo);
    const body = element("div", "card-body");
    body.append(
      element("h3", "", product.name),
      element("p", "muted", [product.type, product.size, product.color].join(" · ")),
    );
    if (product.description) body.append(element("p", "", product.description));
    body.append(element("p", "card-price", rubles(product.price)));
    if (!user || user.role.name === "client") {
      body.append(button("В корзину", "primary", async () => {
        if (!user) {
          openAuth();
          return;
        }
        await api("/cart", "POST", { product_id: product.id, count: 1 });
        notice("Товар добавлен в корзину.");
      }));
    } else if (canEditCatalog()) {
      body.append(
        button("Редактировать", "secondary", () => openProduct(product)),
        button("Удалить товар", "quiet danger", async () => {
          if (!window.confirm("Удалить товар «" + product.name + "» из каталога?")) return;
          await api(route("/product", product.id), "DELETE");
          await refresh();
          notice("Товар удалён.");
        }),
      );
    }
    card.append(picture, body);
    products.append(card);
  }
}

function renderCart(items) {
  const list = byId("cart-items");
  list.replaceChildren();
  byId("checkout-box").hidden = items.length === 0;
  if (!items.length) list.append(element("p", "empty", "Корзина пока пуста."));
  let total = 0;
  for (const item of items) {
    const row = element("article", "row");
    const description = element("div");
    description.append(element("h3", "", item.name), element("p", "muted", rubles(item.price) + " за шт."));
    total += item.price * item.count;
    const form = element("form");
    const label = element("label", "", "Количество");
    const quantity = element("input");
    quantity.type = "number";
    quantity.min = "1";
    quantity.max = String(Number.MAX_SAFE_INTEGER);
    quantity.required = true;
    quantity.value = item.count;
    label.append(quantity);
    const save = element("button", "secondary", "Изменить");
    form.append(label, save);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      run(save, async () => {
        await api(route("/cart", item.id), "PUT", { count: quantity.valueAsNumber });
        await refresh();
      });
    });
    const remove = button("Удалить", "quiet", async () => {
      await api(route("/cart", item.id), "DELETE");
      await refresh();
    });
    row.append(description, form, element("strong", "", rubles(item.price * item.count)), remove);
    list.append(row);
  }
  byId("cart-total").textContent = rubles(total);
}

function renderOrders(page) {
  const list = byId("order-items");
  list.replaceChildren();
  updatePages("order", page);
  if (!page.items?.length) list.append(element("p", "empty", "Заказов пока нет."));
  for (const order of page.items || []) {
    const card = element("article", "order-card");
    const title = element("div", "section-head");
    title.append(element("h3", "", "Заказ " + order.id.slice(0, 8)), element("span", "badge", order.status));
    const details = element("div");
    const actions = element("div", "order-actions");
    const toggle = button("Состав заказа", "secondary", async () => {
      if (details.hasChildNodes()) {
        details.replaceChildren();
        return;
      }
      const full = await api(route("/order", order.id));
      const items = await Promise.all((full.items || []).map(async (item) => {
        const product = await api(route("/product", item.product_id)).catch(() => null);
        return element("li", "", (product?.name || "Товар") + " · " + item.count + " × " + rubles(item.price));
      }));
      const composition = element("ul");
      composition.append(...items);
      details.append(composition);
    });
    actions.append(toggle);
    const next = { "Принят": "В работе", "В работе": "Собран", "Собран": "Готов к выдаче" };
    const canManage = ["admin", "manager"].includes(user?.role.name);
    const change = (status) => button(status === "Отменён" ? "Отменить заказ" : status, "quiet", async () => {
      if (status === "Отменён" && !window.confirm("Отменить этот заказ?")) return;
      await api(route("/order", order.id), "PUT", { status });
      await refresh();
      notice("Статус обновлён.");
    });
    if (canManage && next[order.status]) actions.append(change(next[order.status]));
    if (order.status === "Принят" || (canManage && order.status === "В работе")) actions.append(change("Отменён"));
    const date = new Date(order.datetime).toLocaleString("ru-RU", { dateStyle: "medium", timeStyle: "short" });
    card.append(title, element("p", "muted", date), element("p", "section-head", rubles(order.total)), actions, details);
    list.append(card);
  }
}

function setAuthMode(isRegistering) {
  registering = isRegistering;
  byId("auth-title").textContent = registering ? "Регистрация" : "Вход";
  byId("auth-submit").textContent = registering ? "Зарегистрироваться" : "Войти";
  byId("auth-mode").textContent = registering ? "У меня есть аккаунт" : "Создать аккаунт";
  for (const name of ["name", "phone"]) {
    byId("register-" + name).hidden = !registering;
    const input = authForm.elements.namedItem(name);
    input.disabled = !registering;
    input.required = registering;
  }
  const password = authForm.elements.namedItem("password");
  password.autocomplete = registering ? "new-password" : "current-password";
  notice("", false, byId("auth-error"));
}

function openAuth() {
  authForm.reset();
  setAuthMode(false);
  authDialog.showModal();
}

// Старая карточка может содержать значение, которого нет в новых списках.
// Временно добавляем его в форму, чтобы правка цены не сбросила другие поля.
function openProduct(product) {
  productID = product?.id || null;
  productForm.querySelectorAll("[data-saved-option]").forEach((option) => option.remove());
  productForm.reset();
  photoForm.reset();
  notice("", false, byId("product-error"));
  byId("product-title").textContent = product ? "Редактирование товара" : "Новый товар";
  photoForm.hidden = !product;
  if (product) {
    for (const input of productForm.elements) {
      if (!input.name) continue;
      const value = input.name === "price" ? (product.price / 100).toFixed(2).replace(".", ",") : product[input.name];
      if (input instanceof HTMLSelectElement && !Array.from(input.options).some((option) => option.value === value)) {
        const option = new Option(value, value);
        option.dataset.savedOption = "true";
        input.add(option);
      }
      input.value = value;
    }
  }
  productDialog.showModal();
}

navigation.forEach((item) => item.addEventListener("click", () => run(item, () => show(item.dataset.view))));
byId("analytics-period").addEventListener("change", () => {
  notice("");
  refresh().catch((error) => notice(error.message, true));
});
byId("analytics-toggle").addEventListener("click", (event) => {
  const chart = byId("analytics-chart");
  chart.hidden = !chart.hidden;
  event.currentTarget.textContent = chart.hidden ? "Показать диаграмму" : "Скрыть диаграмму";
  event.currentTarget.setAttribute("aria-expanded", String(!chart.hidden));
});
document.querySelectorAll("[data-close]").forEach((item) => {
  item.addEventListener("click", () => item.closest("dialog").close());
});
byId("account").addEventListener("click", openAuth);
byId("auth-mode").addEventListener("click", () => setAuthMode(!registering));
authForm.addEventListener("submit", (event) => {
  event.preventDefault();
  run(byId("auth-submit"), async () => {
    const data = Object.fromEntries(new FormData(authForm));
    if (registering) {
      const size = new TextEncoder().encode(data.password).length;
      if (size < 12 || size > 1024) {
        throw new Error("Пароль слишком короткий или длинный. Для латиницы допустимо от 12 до 1024 символов.");
      }
    }
    const result = await api(registering ? "/auth/register" : "/auth/login", "POST", data);
    user = result.user;
    renderSession();
    authDialog.close();
    authForm.reset();
    await show("catalog").catch((error) => notice(error.message, true));
  }, byId("auth-error"));
});
byId("logout").addEventListener("click", (event) => run(event.currentTarget, async () => {
  await api("/auth/logout", "POST").catch((error) => {
    if (error.status !== 401) throw error;
  });
  user = null;
  offsets.orders = 0;
  offsets.managers = 0;
  byId("manager-items").replaceChildren();
  byId("analytics-results").hidden = true;
  byId("order-items").replaceChildren();
  byId("cart-items").replaceChildren();
  renderSession();
  await show("catalog");
}));
byId("new-product").addEventListener("click", () => openProduct());
byId("new-manager").addEventListener("click", () => {
  managerForm.reset();
  notice("", false, byId("manager-error"));
  managerDialog.showModal();
});
managerDialog.addEventListener("close", () => managerForm.reset());
managerForm.addEventListener("submit", (event) => {
  event.preventDefault();
  run(event.submitter, async () => {
    const data = Object.fromEntries(new FormData(managerForm));
    const size = new TextEncoder().encode(data.password).length;
    if (size < 12 || size > 1024) throw new Error("Проверьте длину пароля. Для латиницы допустимо от 12 до 1024 символов.");
    data.role_id = await getManagerRoleID();
    await api("/user", "POST", data);
    managerDialog.close();
    offsets.managers = 0;
    notice("Менеджер создан.");
    await refresh().catch((error) => notice(error.message, true));
  }, byId("manager-error"));
});
productForm.addEventListener("submit", (event) => {
  event.preventDefault();
  run(event.submitter, async () => {
    const data = Object.fromEntries(new FormData(productForm));
    const price = data.price.trim().match(/^(\d+)(?:[,.](\d{1,2}))?$/);
    if (!price) throw new Error("Введите цену в рублях, например 1990,50.");
    data.price = Number(price[1]) * 100 + Number((price[2] || "").padEnd(2, "0"));
    if (!Number.isSafeInteger(data.price)) throw new Error("Укажите меньшую цену.");
    await api(productID ? route("/product", productID) : "/product", productID ? "PUT" : "POST", data);
    productDialog.close();
    notice("Товар сохранён. Фото можно загрузить при редактировании.");
    await refresh().catch((error) => notice(error.message, true));
  }, byId("product-error"));
});
photoForm.addEventListener("submit", (event) => {
  event.preventDefault();
  run(event.submitter, async () => {
    const data = new FormData(photoForm);
    if (data.get("file").size > 5 * 1024 * 1024) throw new Error("Выберите JPEG или PNG размером до 5 МиБ.");
    data.append("product_id", productID);
    await api("/photo", "POST", data);
    productDialog.close();
    notice("Фото загружено.");
    await refresh().catch((error) => notice(error.message, true));
  }, byId("product-error"));
});

const filters = byId("filters");
filters.addEventListener("submit", (event) => {
  event.preventDefault();
  catalogFilters = Object.fromEntries(Array.from(new FormData(filters), ([key, value]) => [key, value.trim()]).filter(([, value]) => value));
  offsets.catalog = 0;
  run(event.submitter, () => show("catalog"));
});
filters.addEventListener("reset", () => {
  catalogFilters = {};
  offsets.catalog = 0;
  show("catalog").catch((error) => notice(error.message, true));
});
for (const [prefix, key] of [["catalog", "catalog"], ["order", "orders"], ["manager", "managers"]]) {
  for (const [direction, delta] of [["prev", -pageSize], ["next", pageSize]]) {
    byId(prefix + "-" + direction).addEventListener("click", (event) => {
      run(event.currentTarget, async () => {
        const previous = offsets[key];
        offsets[key] += delta;
        try {
          await refresh();
        } catch (error) {
          offsets[key] = previous;
          throw error;
        }
      });
    });
  }
}
byId("checkout").addEventListener("click", (event) => run(event.currentTarget, async () => {
  const order = await api("/order", "POST");
  offsets.orders = 0;
  try {
    await show("orders");
    notice("Заказ оформлен на " + rubles(order.total) + ".");
  } catch (error) {
    notice("Заказ оформлен. " + error.message, true);
  }
}));

async function init() {
  let sessionError;
  try {
    user = await api("/auth/me");
  } catch (error) {
    if (error.status !== 401) sessionError = error;
  }
  renderSession();
  await show("catalog");
  if (sessionError) notice(sessionError.message, true);
}

init().catch((error) => notice(error.message, true));
