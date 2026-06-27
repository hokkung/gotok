(function() {
  'use strict';

  var me = null;
  var activeConvId = null;
  var activeConvType = null;
  var activeOtherId = null;
  var ws = null;
  var wsReconnectTimer = null;
  var conversations = [];
  var messageCache = {};
  var loadingConvs = false;
  var loadingMsgs = false;
  var convCursor = 0;
  var msgCursor = 0;

  function escapeHtml(s) {
    var d = document.createElement('div');
    d.textContent = s || '';
    return d.innerHTML;
  }

  function avatarHtml(url, name) {
    if (url) {
      return '<img class="avatar-img" src="' + escapeHtml(url) + '" alt="">';
    }
    return '<div class="avatar">' + escapeHtml((name || '?').slice(0, 1).toUpperCase()) + '</div>';
  }

  function timeAgo(ts) {
    var diff = Date.now() / 1000 - ts;
    if (diff < 60) return 'now';
    if (diff < 3600) return Math.floor(diff / 60) + 'm';
    if (diff < 86400) return Math.floor(diff / 3600) + 'h';
    if (diff < 604800) return Math.floor(diff / 86400) + 'd';
    return new Date(ts * 1000).toLocaleDateString();
  }

  function formatTime(ts) {
    var d = new Date(ts * 1000);
    var h = d.getHours();
    var m = d.getMinutes();
    return (h < 10 ? '0' : '') + h + ':' + (m < 10 ? '0' : '') + m;
  }

  // ---- WebSocket ----

  function connectWS() {
    var proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(proto + '//' + window.location.host + '/ws');

    ws.onopen = function() {
      if (wsReconnectTimer) { clearTimeout(wsReconnectTimer); wsReconnectTimer = null; }
    };

    ws.onmessage = function(ev) {
      var data;
      try { data = JSON.parse(ev.data); } catch (e) { return; }
      handleWSMessage(data);
    };

    ws.onclose = function() {
      ws = null;
      if (!wsReconnectTimer) {
        wsReconnectTimer = setTimeout(connectWS, 3000);
      }
    };
  }

  function wsSend(obj) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(obj));
      return true;
    }
    return false;
  }

  function handleWSMessage(data) {
    switch (data.type) {
      case 'message':
        receiveMessage(data);
        break;
      case 'read_receipt':
        updateReadReceipt(data);
        break;
      case 'presence':
        updatePresence(data);
        break;
    }
  }

  // ---- Conversations ----

  async function loadConversations(append) {
    if (loadingConvs) return;
    loadingConvs = true;
    document.getElementById('chatListLoader').hidden = false;
    try {
      var url = '/api/conversations?limit=20';
      if (append && convCursor) url += '&cursor=' + convCursor;
      var res = await fetch(url);
      if (res.status === 401) { window.location.href = '/login?next=/chat'; return; }
      var data = await res.json();
      if (!res.ok) return;
      var list = data.conversations || [];
      if (!append) {
        conversations = list;
        renderConversationList();
      } else {
        conversations = conversations.concat(list);
        appendConversationList(list);
      }
      convCursor = data.next || 0;
      if (!append && conversations.length === 0) {
        document.getElementById('chatList').innerHTML =
          '<div class="chat-empty"><span class="chat-empty-icon">💬</span>' +
          '<p>No conversations yet</p>' +
          '<span>Visit a user\'s profile to start chatting.</span></div>';
      }
    } catch (e) {
      console.error(e);
    } finally {
      loadingConvs = false;
      document.getElementById('chatListLoader').hidden = true;
    }
  }

  function renderConversationList() {
    var el = document.getElementById('chatList');
    el.innerHTML = '';
    conversations.forEach(function(c) { el.appendChild(renderConvItem(c)); });
  }

  function appendConversationList(items) {
    var el = document.getElementById('chatList');
    items.forEach(function(c) { el.appendChild(renderConvItem(c)); });
  }

  function renderConvItem(c) {
    var div = document.createElement('div');
    div.className = 'conv-item' + (c.id === activeConvId ? ' active' : '');
    var name = c.type === 'group' ? (c.title || 'Group') : (c.other_user_name || 'Unknown');
    var avatar = c.type === 'group' ? '' : c.other_avatar;
    var onlineDot = (c.type === 'dm' && c.online) ? '<span class="online-dot"></span>' : '';
    var unread = c.unread_count > 0 ? '<span class="unread-badge">' + c.unread_count + '</span>' : '';
    var preview = c.last_msg_text ? escapeHtml(c.last_msg_text) : '<span style="color:#666">No messages yet</span>';
    div.innerHTML =
      '<div class="conv-avatar-wrap">' + avatarHtml(avatar, name) + onlineDot + '</div>' +
      '<div class="conv-info">' +
        '<div class="conv-top"><span class="conv-name">' + escapeHtml(name) + unread + '</span>' +
          '<span class="conv-time">' + (c.last_msg_at ? timeAgo(c.last_msg_at) : '') + '</span></div>' +
        '<div class="conv-preview">' + preview + '</div>' +
      '</div>';
    div.addEventListener('click', function() { openConversation(c); });
    return div;
  }

  function openConversation(c) {
    activeConvId = c.id;
    activeConvType = c.type;
    activeOtherId = c.other_user_id || 0;
    messageCache[c.id] = messageCache[c.id] || [];
    msgCursor = 0;

    document.querySelectorAll('.conv-item').forEach(function(el) { el.classList.remove('active'); });

    document.getElementById('chatThreadEmpty').hidden = true;
    document.getElementById('chatThread').hidden = false;

    var name = c.type === 'group' ? (c.title || 'Group') : (c.other_user_name || 'Unknown');
    document.getElementById('threadName').textContent = name;
    document.getElementById('threadAvatar').innerHTML = avatarHtml(c.other_avatar, name);
    document.getElementById('threadStatus').textContent = c.online ? 'Online' : '';

    document.getElementById('messageList').innerHTML = '';
    loadMessages(c.id, false);

    markRead(c.id);
    document.querySelector('.chat-app').classList.add('thread-open');
    document.getElementById('messageInput').focus();
  }

  // ---- Messages ----

  async function loadMessages(convId, append) {
    if (loadingMsgs) return;
    loadingMsgs = true;
    document.getElementById('messageLoader').hidden = false;
    try {
      var url = '/api/conversations/' + convId + '/messages?limit=50';
      if (append && msgCursor) url += '&before=' + msgCursor;
      var res = await fetch(url);
      var data = await res.json();
      if (!res.ok) return;
      var msgs = data.messages || [];
      msgCursor = data.next || 0;

      var list = document.getElementById('messageList');
      if (!append) {
        messageCache[convId] = msgs;
        list.innerHTML = '';
        msgs.reverse().forEach(function(m) { list.appendChild(renderMessage(m)); });
        list.scrollTop = list.scrollHeight;
      } else {
        var wasNearTop = list.scrollTop < 100;
        msgs.reverse().forEach(function(m) { list.insertBefore(renderMessage(m), list.firstChild); });
        messageCache[convId] = msgs.concat(messageCache[convId] || []);
        if (wasNearTop) list.scrollTop = 0;
      }
    } catch (e) {
      console.error(e);
    } finally {
      loadingMsgs = false;
      document.getElementById('messageLoader').hidden = true;
    }
  }

  function renderMessage(m) {
    var isSelf = me && m.sender_id === me.id;
    var div = document.createElement('div');
    div.className = 'msg-item' + (isSelf ? ' self' : '');
    var avatar = isSelf ? '' : '<div class="msg-avatar">' + avatarHtml(m.sender_avatar, m.sender_name) + '</div>';
    var name = isSelf ? '' : '<span class="msg-sender">' + escapeHtml(m.sender_name) + '</span>';
    div.innerHTML =
      avatar +
      '<div class="msg-bubble-wrap">' +
        name +
        '<div class="msg-bubble">' + escapeHtml(m.text) + '</div>' +
        '<span class="msg-time">' + formatTime(m.created_at) + '</span>' +
      '</div>';
    div.setAttribute('data-msg-id', m.id);
    return div;
  }

  function receiveMessage(data) {
    if (!data.conversation_id) return;

    // Update conversation list: move to top, update preview.
    var conv = conversations.find(function(c) { return c.id === data.conversation_id; });
    if (conv) {
      conv.last_msg_text = data.text;
      conv.last_msg_at = data.created_at;
      conv.last_msg_sender = data.sender_id;
      var idx = conversations.indexOf(conv);
      conversations.splice(idx, 1);
      conversations.unshift(conv);
      renderConversationList();
    }

    // Render in the active thread if applicable.
    if (data.conversation_id === activeConvId) {
      var list = document.getElementById('messageList');
      var wasAtBottom = list.scrollTop + list.clientHeight >= list.scrollHeight - 50;
      list.appendChild(renderMessage(data));
      if (wasAtBottom) list.scrollTop = list.scrollHeight;
      markRead(activeConvId);
    } else {
      // Increment unread count for the conversation.
      if (conv && (!me || data.sender_id !== me.id)) {
        conv.unread_count = (conv.unread_count || 0) + 1;
      }
    }
  }

  function sendMessage() {
    var input = document.getElementById('messageInput');
    var text = input.value.trim();
    if (!text || !activeConvId) return;
    input.value = '';

    if (wsSend({ type: 'message', conversation_id: activeConvId, text: text })) {
      return;
    }

    // REST fallback if WS is not connected.
    fetch('/api/conversations/' + activeConvId + '/messages', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text: text })
    }).then(function(res) { return res.json(); }).then(function(data) {
      if (data.message) receiveMessage(data.message);
    }).catch(function(err) { console.error(err); input.value = text; });
  }

  // ---- Read receipts ----

  function markRead(convId) {
    var msgs = messageCache[convId] || [];
    if (msgs.length === 0) return;
    var lastId = msgs[msgs.length - 1].id;

    wsSend({ type: 'read', conversation_id: convId, message_id: lastId });

    fetch('/api/conversations/' + convId + '/read', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message_id: lastId })
    }).catch(function() {});

    var conv = conversations.find(function(c) { return c.id === convId; });
    if (conv) { conv.unread_count = 0; renderConversationList(); }
  }

  function updateReadReceipt(data) {
    if (data.conversation_id !== activeConvId) return;
    var status = document.getElementById('threadStatus');
    if (status) status.textContent = 'Seen';
  }

  function updatePresence(data) {
    if (data.user_id === activeOtherId) {
      var status = document.getElementById('threadStatus');
      if (status) status.textContent = data.online ? 'Online' : '';
    }
    var conv = conversations.find(function(c) { return c.other_user_id === data.user_id; });
    if (conv) { conv.online = data.online; renderConversationList(); }
  }

  // ---- Init ----

  async function init() {
    try {
      var res = await fetch('/api/me');
      var data = await res.json();
      me = data.user;
      if (!me) { window.location.href = '/login?next=/chat'; return; }
    } catch (e) { return; }

    await loadConversations(false);
    connectWS();

    var params = new URLSearchParams(window.location.search);
    var convId = parseInt(params.get('c'), 10);
    if (convId) {
      var conv = conversations.find(function(c) { return c.id === convId; });
      if (conv) {
        openConversation(conv);
      } else {
        var c = { id: convId, type: 'dm', other_user_name: '', other_avatar: '' };
        openConversation(c);
      }
    }

    // Infinite scroll for conversations.
    var chatListEl = document.getElementById('chatList');
    chatListEl.addEventListener('scroll', function() {
      if (chatListEl.scrollTop + chatListEl.clientHeight >= chatListEl.scrollHeight - 100) {
        if (convCursor) loadConversations(true);
      }
    });

    // Infinite scroll for messages (load older).
    var msgListEl = document.getElementById('messageList');
    msgListEl.addEventListener('scroll', function() {
      if (msgListEl.scrollTop < 100 && msgCursor) loadMessages(activeConvId, true);
    });

    // Send button + Enter key.
    document.getElementById('messageSend').addEventListener('click', sendMessage);
    document.getElementById('messageInput').addEventListener('keydown', function(e) {
      if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMessage(); }
    });

    // Back button (mobile).
    document.getElementById('chatBackBtn').addEventListener('click', function() {
      document.querySelector('.chat-app').classList.remove('thread-open');
      activeConvId = null;
      document.getElementById('chatThread').hidden = true;
      document.getElementById('chatThreadEmpty').hidden = false;
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
