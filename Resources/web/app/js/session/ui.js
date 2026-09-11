/* The dependency-inversion seam between session policy/controllers and DOM renderers. */

var callbacks = Object.freeze(Object.create(null));

export function bindSessionUI(bindings) {
    if (!bindings || typeof bindings !== "object" || Array.isArray(bindings)) {
        throw new TypeError("session UI bindings object required");
    }
    var next = Object.create(null);
    Object.keys(bindings).forEach(function (name) {
        if (!name) throw new TypeError("session UI binding name required");
        if (typeof bindings[name] !== "function") {
            throw new TypeError("session UI binding " + name + " must be a function");
        }
        next[name] = bindings[name];
    });
    callbacks = Object.freeze(next);
}

export function sessionUICallback(name) {
    if (typeof name !== "string" || !name) throw new TypeError("session UI callback name required");
    if (typeof callbacks[name] !== "function") {
        throw new Error("session UI callback " + name + " is not bound");
    }
    return callbacks[name];
}

export function callSessionUI(name) {
    var callback = sessionUICallback(name);
    return callback.apply(null, Array.prototype.slice.call(arguments, 1));
}
