import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createImageLightbox, createImageZoom } from '../legacy/js/view/transcript-images.js';

class Node {
    dataset = {};
    style = {};
    hidden = false;
    listeners = new Map();
    focused = false;
    addEventListener(kind, fn) {
        const list = this.listeners.get(kind) || [];
        list.push(fn);
        this.listeners.set(kind, list);
    }
    emit(kind, fields = {}) {
        const event = { target: this, preventDefault() {}, stopPropagation() {}, ...fields };
        for (const fn of this.listeners.get(kind) || []) fn(event);
    }
    setAttribute() {}
    removeAttribute() {}
    focus() { this.focused = true; }
    getBoundingClientRect() { return { left: 0, top: 0, width: 400, height: 600 }; }
}

function preview() {
    const doc = new Node(), dialog = new Node(), frame = new Node(), image = new Node(), close = new Node();
    dialog.querySelector = () => frame;
    Object.assign(image, { offsetWidth: 400, offsetHeight: 600, naturalWidth: 800, naturalHeight: 1200 });
    const box = createImageLightbox(dialog, image, close, doc);
    box.open(new Node(), 'picture.png');
    image.emit('load');
    return { doc, dialog, frame, image, close, box };
}

const geometry = { frame: { left: 0, top: 0, width: 400, height: 600 }, content: { width: 400, height: 600 }, natural: { width: 800 } };

test('shrinking reaches 75%, survives remeasurement and recentres', () => {
    const zoom = createImageZoom();
    zoom.layout(geometry);
    zoom.scaleTo(2, 100, 200);
    zoom.panBy(40, 70);
    zoom.scaleTo(0.5, 100, 200);
    assert.deepEqual(zoom.state(), { scale: 0.75, x: 0, y: 0 });
    zoom.layout(geometry);
    assert.equal(zoom.state().scale, 0.75);
    assert.equal(zoom.canPan(), false);
    zoom.toggle(200, 300);
    assert.equal(zoom.state().scale, 2.5);
    zoom.toggle(200, 300);
    assert.equal(zoom.state().scale, 1);
});

test('a shrunken long screenshot retains bounded vertical reading', () => {
    const zoom = createImageZoom();
    zoom.layout({ ...geometry, content: { width: 400, height: 2000 } });
    zoom.alignTop();
    zoom.scaleTo(0.75);
    assert.equal(zoom.canPan(), true);
    assert.deepEqual(zoom.state(), { scale: 0.75, x: 0, y: 450 });
    zoom.panBy(900, -10000);
    assert.deepEqual(zoom.state(), { scale: 0.75, x: 0, y: -450 });
});

test('single taps toggle controls; keyboard and a new image restore them', t => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const { dialog, image, doc, box } = preview();
    dialog.emit('click', { target: image, detail: 1 });
    t.mock.timers.tick(321);
    assert.equal(dialog.dataset.controlsHidden, 'true');
    assert.equal(dialog.focused, true);
    dialog.emit('click', { target: image, detail: 1 });
    t.mock.timers.tick(321);
    assert.equal(dialog.dataset.controlsHidden, 'false');
    dialog.emit('click', { target: image, detail: 1 });
    t.mock.timers.tick(321);
    doc.emit('keydown', { key: 'Tab' });
    assert.equal(dialog.dataset.controlsHidden, 'false');
    dialog.emit('click', { target: image, detail: 1 });
    box.close();
    box.open(new Node(), 'next.png');
    t.mock.timers.tick(321);
    assert.equal(dialog.dataset.controlsHidden, 'false');
    doc.emit('keydown', { key: 'Escape' });
    assert.equal(dialog.hidden, true);
});

test('double clicks, drags and action clicks never toggle controls', t => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const { dialog, image, doc, close } = preview();
    dialog.emit('click', { target: image, detail: 1 });
    dialog.emit('click', { target: image, detail: 2 });
    image.emit('dblclick', { clientX: 200, clientY: 300 });
    t.mock.timers.tick(321);
    assert.equal(dialog.dataset.controlsHidden, 'false');
    assert.match(image.style.transform, /scale\(2.5\)/);
    dialog.emit('pointerdown', { clientX: 200, clientY: 300 });
    doc.emit('pointermove', { clientX: 250, clientY: 350 });
    doc.emit('pointerup');
    dialog.emit('click', { target: image, detail: 1 });
    dialog.emit('click', { target: close, detail: 1 });
    t.mock.timers.tick(321);
    assert.equal(dialog.dataset.controlsHidden, 'false');
});

test('wheel and two-finger pinch can shrink to 75%', () => {
    const { dialog, image } = preview();
    dialog.emit('wheel', { deltaY: 2000, clientX: 200, clientY: 300 });
    assert.match(image.style.transform, /scale\(0.75\)/);
    const p = preview();
    const point = (x) => ({ clientX: x, clientY: 300 });
    p.dialog.emit('touchstart', { target: p.image, touches: [point(100), point(300)] });
    p.dialog.emit('touchmove', { touches: [point(180), point(220)] });
    assert.match(p.image.style.transform, /scale\(0.75\)/);
});

test('two touch taps zoom without concealing controls', t => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const { dialog, image } = preview();
    const point = { clientX: 200, clientY: 300 };
    const tap = () => {
        dialog.emit('touchstart', { target: image, touches: [point] });
        dialog.emit('touchend', { target: image, touches: [], changedTouches: [point] });
    };
    tap();
    tap();
    t.mock.timers.tick(321);
    assert.equal(dialog.dataset.controlsHidden, 'false');
    assert.match(image.style.transform, /scale\(2.5\)/);
});


test('a native mouse double click restores controls when its interval exceeds the tap timer', t => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const { dialog, image } = preview();
    dialog.emit('click', { target: image, detail: 1 });
    t.mock.timers.tick(321);
    assert.equal(dialog.dataset.controlsHidden, 'true');
    dialog.emit('click', { target: image, detail: 2 });
    image.emit('dblclick', { clientX: 200, clientY: 300 });
    assert.equal(dialog.dataset.controlsHidden, 'false');
    assert.match(image.style.transform, /scale\(2.5\)/);
});

test('a touch single tap toggles controls without a synthetic mouse click', t => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const { dialog, image } = preview();
    const point = { clientX: 200, clientY: 300 };
    let prevented = false;
    dialog.emit('touchstart', { target: image, touches: [point] });
    dialog.emit('touchend', { target: image, touches: [], changedTouches: [point], preventDefault() { prevented = true; } });
    t.mock.timers.tick(321);
    assert.equal(prevented, true);
    assert.equal(dialog.dataset.controlsHidden, 'true');
});
