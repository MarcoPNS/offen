/**
 * Copyright 2020 - Offen Authors <hioffen@posteo.de>
 * SPDX-License-Identifier: Apache-2.0
 */

var cookies = require('./cookie-tools')

var token = '__allows_cookies__'

module.exports = allowsCookies

function allowsCookies () {
  var isLocalhost = window.location.hostname === 'localhost'
  var sameSite = isLocalhost ? 'Lax' : 'None'
  document.cookie = cookies.serialize({
    ok: token, SameSite: sameSite, Secure: !isLocalhost
  })
  var support = document.cookie.indexOf(token) >= 0
  document.cookie = cookies.serialize({
    ok: '', expires: new Date(0).toUTCString(), SameSite: sameSite, Secure: !isLocalhost
  })
  return support
}
