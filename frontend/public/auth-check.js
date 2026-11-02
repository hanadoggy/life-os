(async function checkSession() {
  const statusEl = document.getElementById("status-text");
  const actionEl = document.getElementById("action-area");
  try {
    const res = await fetch("/api/v1/auth/me", { credentials: "same-origin" });
    if (res.ok) {
      const data = await res.json();
      statusEl.textContent = "ログイン済み: " + data.email;
      const btn = document.createElement("button");
      btn.className = "btn btn-secondary";
      btn.textContent = "ログアウト";
      btn.onclick = async () => {
        await fetch("/api/v1/auth/logout", {
          method: "POST",
          credentials: "same-origin",
        });
        window.location.reload();
      };
      actionEl.replaceChildren(btn);
    } else {
      statusEl.textContent = "Googleアカウントでログインしてください";
      const link = document.createElement("a");
      link.className = "btn";
      link.href = "/auth/google/login";
      link.textContent = "Googleでログイン";
      actionEl.replaceChildren(link);
    }
  } catch (err) {
    statusEl.textContent = "通信エラーが発生しました";
  }
})();

