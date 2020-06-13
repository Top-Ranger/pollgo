/**
 * @license
 * abbr-touch <http://github.com/Tyriar/abbr-touch>
 * Copyright 2014 Daniel Imms <http://www.growingwiththeweb.com>
 * Released under the MIT license <http://github.com/Tyriar/abbr-touch/blob/master/LICENSE>
 * 
 * The MIT License (MIT)
 *
 * Copyright (c) 2014 Daniel Imms, http://www.growingwiththeweb.com
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
 * THE SOFTWARE.
 */
var abbrTouch = (function () { // eslint-disable-line no-unused-vars
  'use strict';

  /**
   * Generates a touchtap event handler that calls the tap handler provided.
   * @param {function} handler The tap handler to call.
   * @returns {function}
   */
  function generateTouchtapHandler(handler) {
    return function (e) {
      handler(e.currentTarget, e.currentTarget.title, e.customData.touchX, e.customData.touchY);
    };
  }

  /**
   * The default lightweight tap handler.
   */
  function defaultOnTapHandler(target, title, touchX, touchY) { // eslint-disable-line no-unused-vars
    alert(title); // eslint-disable-line no-alert
  }

  /**
   * Attaches abbrTouch events on all abbr[title] elements within an element
   * @param {HTMLElement} elementScope The element containing abbr[title]
   * elements.
   * @param {function} customTapHandler (Optional) A custom touchtap handler to
   * be used when abbr[title] elements are touched.
   */
  function init(elementScope, customTapHandler) {
    try {
      if (!elementScope) {
        elementScope = document;
      }

      var tapHandler = customTapHandler || defaultOnTapHandler;

      var elements = elementScope.querySelectorAll('abbr[title]');
      var touchtapHandler = generateTouchtapHandler(tapHandler);
      for (var i = 0; i < elements.length; i++) {
        // EventTarget.addEventListener() is supported by most browsers, including IE9 and newer.
        if (elements[i].addEventListener) {
          elements[i].addEventListener('touchtap', touchtapHandler, false);
        }
        else {
          // EventTarget.addEventListener() is not implemented in this browser.
          // https://developer.mozilla.org/en-US/docs/Web/API/EventTarget/addEventListener#Browser_compatibility
          break;
        }
      }
    }
    catch (err) {
      // Some old browsers do not support Document.querySelectorAll() - e.g. prior to IE9.
      // https://developer.mozilla.org/en-US/docs/Web/API/Document/querySelectorAll#Browser_compatibility
    }
  }
  return init;
})();
