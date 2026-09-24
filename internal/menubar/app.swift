// agtop's menu bar icon. Built by `agtop menubar`; everything it shows
// comes from `agtop menubar feed`, which it runs and talks to one JSON
// object per line.

import AppKit
import UserNotifications

struct Win: Decodable {
    var present: Bool
    var percent: Double
    var resetsAt: String?
}

struct Acct: Decodable {
    var name: String
    var email: String?
    var plan: String?
    var fiveHour: Win
    var sevenDay: Win
    var problem: String?
    var live: Int
    var current: Bool?
}

struct Work: Decodable {
    var key: String
    var name: String
    var doing: String
    var repo: String?
    var account: String
    var since: String
}

struct Wait: Decodable {
    var key: String
    var name: String
    var needs: String
    var account: String
    var seen: Bool?
    var kind: String?
    var req: String?
    var header: String?
    var text: String?
    var options: [String]?
    var since: String

    var sig: String { req ?? needs }
}

struct FeedLine: Decodable {
    var accounts: [Acct]?
    var working: [Work]?
    var more: Int?
    var waiting: [Wait]?
    var error: String?
}

// Formatters are made once: each is a good deal of ICU to set up.
let iso = ISO8601DateFormatter()
let clock = formatter("HH:mm"), dayClock = formatter("EEE HH:mm")

func formatter(_ format: String) -> DateFormatter {
    let f = DateFormatter()
    f.dateFormat = format
    return f
}

func parseTime(_ s: String?) -> Date? {
    guard var s = s, !s.hasPrefix("0001") else { return nil }
    // Go writes nanoseconds, which the ISO 8601 parser doesn't take.
    if let r = s.range(of: #"\.\d+"#, options: .regularExpression) { s.removeSubrange(r) }
    return iso.date(from: s)
}

func ago(_ s: String) -> String {
    guard let d = parseTime(s) else { return "" }
    let m = max(0, Int(Date().timeIntervalSince(d) / 60))
    if m < 60 { return "\(m)m" }
    if m < 60 * 24 { return m % 60 == 0 ? "\(m / 60)h" : "\(m / 60)h \(m % 60)m" }
    return "\(m / 60 / 24)d"
}

func resets(_ s: String?) -> String {
    guard let d = parseTime(s) else { return "" }
    return (Calendar.current.isDateInToday(d) ? clock : dayClock).string(from: d)
}

final class LineBuffer {
    var data = Data()
    func lines(adding d: Data) -> [Data] {
        data.append(d)
        var out: [Data] = []
        while let i = data.firstIndex(of: 10) {
            out.append(data.subdata(in: data.startIndex..<i))
            data.removeSubrange(data.startIndex...i)
        }
        return out
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate, NSMenuDelegate, UNUserNotificationCenterDelegate {
    // A new icon goes left of every other, which on a Mac with a notch
    // is often behind it; this one starts near the clock instead, and
    // stays wherever you ⌘-drag it after.
    lazy var item: NSStatusItem = {
        UserDefaults.standard.register(defaults: ["NSStatusItem Preferred Position agtop": 120])
        let i = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        i.autosaveName = "agtop"
        i.isVisible = true
        return i
    }()
    let menu = NSMenu()
    let center = UNUserNotificationCenter.current()
    let bin = Bundle.main.object(forInfoDictionaryKey: "AgtopBinary") as? String ?? "agtop"

    var feed: Process?
    var input: FileHandle?
    var state = FeedLine()
    var got = false // a first state has come; what's waiting then isn't news
    var problem: String?
    var notified: [String: String] = [:] // key → what it was asking
    var categories: [String: UNNotificationCategory] = [:]
    var drawn: String? // what the icon shows, so drawing it again the same is skipped
    var tick: Timer?

    func applicationDidFinishLaunching(_ n: Notification) {
        if !alone() { return }
        menu.delegate = self
        menu.autoenablesItems = false
        item.menu = menu
        center.delegate = self
        center.requestAuthorization(options: [.alert, .sound, .badge]) { _, _ in }
        draw()
        startFeed()
    }

    func applicationWillTerminate(_ n: Notification) {
        feed?.terminate()
    }

    /// alone keeps to one icon: agtops starting together, or a new build
    /// opened over an old, can each launch one. The newest stays and the
    /// rest quit; false when this one is quitting.
    func alone() -> Bool {
        let me = NSRunningApplication.current
        let id = Bundle.main.bundleIdentifier ?? "dev.agtop.menubar"
        let others = NSRunningApplication.runningApplications(withBundleIdentifier: id)
            .filter { $0.processIdentifier != me.processIdentifier }
        func newer(_ a: NSRunningApplication, than b: NSRunningApplication) -> Bool {
            let (x, y) = (a.launchDate ?? .distantPast, b.launchDate ?? .distantPast)
            return x != y ? x > y : a.processIdentifier > b.processIdentifier
        }
        if others.contains(where: { newer($0, than: me) }) {
            NSApp.terminate(nil)
            return false
        }
        others.forEach { $0.terminate() }
        return true
    }

    // MARK: feed

    func startFeed() {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.arguments = ["menubar", "feed"]
        var env = ProcessInfo.processInfo.environment
        env["AGTOP_MENUBAR_PID"] = String(getpid())
        p.environment = env
        let out = Pipe(), inp = Pipe()
        p.standardOutput = out
        p.standardInput = inp
        p.standardError = FileHandle.nullDevice
        let buf = LineBuffer(), decoder = JSONDecoder()
        out.fileHandleForReading.readabilityHandler = { [weak self] h in
            let d = h.availableData
            if d.isEmpty {
                h.readabilityHandler = nil
                return
            }
            for l in buf.lines(adding: d) {
                guard let f = try? decoder.decode(FeedLine.self, from: l) else { continue }
                DispatchQueue.main.async { self?.apply(f) }
            }
        }
        p.terminationHandler = { [weak self] _ in
            DispatchQueue.main.async { self?.feedEnded() }
        }
        do {
            try p.run()
            (feed, input, problem) = (p, inp.fileHandleForWriting, nil)
        } catch {
            problem = "agtop isn't at \(bin) any more: run agtop menubar again"
            draw()
        }
    }

    func feedEnded() {
        (feed, input) = (nil, nil)
        if problem == nil { problem = "lost touch with agtop; trying again…" }
        draw()
        DispatchQueue.main.asyncAfter(deadline: .now() + 5) { [weak self] in
            if self?.feed == nil { self?.startFeed() }
        }
    }

    func send(_ op: [String: Any]) {
        guard let input, let b = try? JSONSerialization.data(withJSONObject: op) else { return }
        input.write(b + Data([10]))
    }

    func apply(_ f: FeedLine) {
        if let e = f.error {
            post(id: "error", title: "agtop", subtitle: nil, body: e, category: nil, info: [:])
            return
        }
        problem = nil
        state = f
        notifyWaiting()
        got = true
        draw()
    }

    // MARK: icon

    func draw() {
        guard let b = item.button else { return }
        let waiting = state.waiting?.count ?? 0
        let working = state.working?.count ?? 0
        // He's clanker, as in agtop's header: working he steps from one
        // pose to the other every other second, and needing you he holds
        // his arms up by a !, in orange.
        let pose = waiting > 0 ? "needs" : working > 0 && Int(Date().timeIntervalSince1970) / 2 % 2 == 1 ? "working" : "idle"
        animate(waiting == 0 && working > 0)
        // Setting the button's image or title has the menu bar lay it out
        // and draw it again, even when they're what they were.
        let look = "\(pose) \(waiting) \(problem != nil)"
        guard look != drawn else { return }
        drawn = look
        b.image = pose == "needs" ? needsImage : pose == "working" ? workingImage : idleImage
        b.appearsDisabled = problem != nil
        b.imagePosition = .imageLeading
        b.attributedTitle = waiting > 0
            ? NSAttributedString(string: " \(waiting)", attributes: [
                .foregroundColor: NSColor.systemOrange,
                .font: NSFont.monospacedDigitSystemFont(ofSize: NSFont.systemFontSize, weight: .semibold),
            ])
            : NSAttributedString(string: "")
        b.toolTip = waiting > 0 ? "\(waiting) waiting on you" : "agtop"
    }

    /// animate ticks while he's working, on the even seconds his pose
    /// changes at, and not at all otherwise.
    func animate(_ on: Bool) {
        guard on != (tick != nil) else { return }
        tick?.invalidate()
        tick = nil
        guard on else { return }
        let next = Date(timeIntervalSince1970: (Date().timeIntervalSince1970 / 2).rounded(.down) * 2 + 2)
        let t = Timer(fire: next, interval: 2, repeats: true) { [weak self] _ in self?.draw() }
        t.tolerance = 0.1
        RunLoop.main.add(t, forMode: .default)
        tick = t
    }

    lazy var idleImage = clanker("idle")
    lazy var workingImage = clanker("working")
    lazy var needsImage = clanker("needs").map { tinted($0, .systemOrange) }

    func clanker(_ pose: String) -> NSImage? {
        let i = Bundle.main.image(forResource: "clanker-\(pose)Template")
        i?.isTemplate = true
        i?.accessibilityDescription = "agtop"
        return i
    }

    func tinted(_ i: NSImage, _ c: NSColor) -> NSImage {
        let t = NSImage(size: i.size, flipped: false) { r in
            i.draw(in: r)
            c.set()
            r.fill(using: .sourceAtop)
            return true
        }
        t.accessibilityDescription = i.accessibilityDescription
        return t
    }

    // MARK: menu

    func menuNeedsUpdate(_ menu: NSMenu) {
        menu.removeAllItems()
        if let problem {
            menu.addItem(view(RowView(title: problem, dot: .systemYellow)))
            menu.addItem(.separator())
        }
        let waiting = state.waiting ?? [], working = state.working ?? []
        if !waiting.isEmpty {
            menu.addItem(.sectionHeader(title: "Needs You"))
            for w in waiting {
                menu.addItem(view(RowView(title: w.name, detail: w.text ?? w.needs, trailing: ago(w.since),
                                          dot: .systemOrange, action: { [weak self] in self?.show(w.key) })))
                for (label, op) in actions(w) {
                    let i = NSMenuItem(title: label, action: #selector(answerFromMenu(_:)), keyEquivalent: "")
                    i.target = self
                    i.representedObject = op
                    i.indentationLevel = 1
                    menu.addItem(i)
                }
            }
        }
        if !working.isEmpty {
            if !waiting.isEmpty { menu.addItem(.separator()) }
            menu.addItem(.sectionHeader(title: "Working"))
            for w in working {
                menu.addItem(view(RowView(title: w.name, detail: w.doing, trailing: ago(w.since),
                                          dot: .systemGreen, action: { [weak self] in self?.show(w.key) })))
            }
            if let more = state.more, more > 0 { menu.addItem(view(RowView(title: "", detail: "and \(more) more"))) }
        }
        if waiting.isEmpty && working.isEmpty && got {
            menu.addItem(view(RowView(title: "", detail: "Nothing running")))
        }
        let accounts = state.accounts ?? []
        if !accounts.isEmpty {
            menu.addItem(.separator())
            menu.addItem(.sectionHeader(title: "Usage"))
            for a in accounts {
                let i = view(UsageView(a))
                i.toolTip = usageTip(a)
                menu.addItem(i)
            }
        }
        menu.addItem(.separator())
        let open = NSMenuItem(title: "Open agtop", action: #selector(openAgtop), keyEquivalent: "o")
        open.target = self
        menu.addItem(open)
        let login = NSMenuItem(title: "Open at Login", action: #selector(toggleLogin), keyEquivalent: "")
        login.target = self
        login.state = FileManager.default.fileExists(atPath: loginPlist.path) ? .on : .off
        menu.addItem(login)
        menu.addItem(NSMenuItem(title: "Quit", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"))
    }

    func view(_ v: NSView) -> NSMenuItem {
        let i = NSMenuItem()
        i.view = v
        return i
    }

    func usageTip(_ a: Acct) -> String {
        var t: [String] = []
        if let e = a.email { t.append(e) }
        if let p = a.problem, !p.isEmpty { t.append(p) }
        if a.fiveHour.present, !resets(a.fiveHour.resetsAt).isEmpty { t.append("5-hour resets " + resets(a.fiveHour.resetsAt)) }
        if a.sevenDay.present, !resets(a.sevenDay.resetsAt).isEmpty { t.append("Week resets " + resets(a.sevenDay.resetsAt)) }
        if a.live > 0 { t.append("\(a.live) running") }
        return t.joined(separator: "\n")
    }

    /// actions are the answers a wait takes from outside agtop, as button
    /// titles and the op each sends.
    func actions(_ w: Wait) -> [(String, [String: Any])] {
        var base: [String: Any] = ["key": w.key]
        if let r = w.req { base["req"] = r }
        func op(_ o: [String: Any]) -> [String: Any] { base.merging(o) { $1 } }
        switch w.kind {
        case "question":
            return (w.options ?? []).prefix(9).map { ($0, op(["op": "answer", "answer": $0])) }
        case "permission":
            return [("Allow", op(["op": "allow"])), ("Deny", op(["op": "deny"]))]
        case "limit":
            return [("Continue when it resets", op(["op": "limit", "yes": true])), ("Wait for me", op(["op": "limit", "yes": false]))]
        default:
            return []
        }
    }

    @objc func answerFromMenu(_ sender: NSMenuItem) {
        guard let op = sender.representedObject as? [String: Any] else { return }
        send(op)
    }

    @objc func openAgtop() { show(nil) }

    /// show brings agtop forward, on the agent key: the feed finds the
    /// terminal it's open in, or opens it in the one it last was.
    func show(_ key: String?) {
        if input != nil {
            send(["op": "show", "key": key ?? ""])
            return
        }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/open")
        p.arguments = [bin]
        try? p.run()
    }

    var loginPlist: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/LaunchAgents/\(Bundle.main.bundleIdentifier ?? "dev.agtop.menubar").plist")
    }

    @objc func toggleLogin() {
        let fm = FileManager.default
        if fm.fileExists(atPath: loginPlist.path) {
            try? fm.removeItem(at: loginPlist)
            return
        }
        let plist: [String: Any] = [
            "Label": Bundle.main.bundleIdentifier ?? "dev.agtop.menubar",
            "ProgramArguments": ["/usr/bin/open", Bundle.main.bundlePath],
            "RunAtLoad": true,
        ]
        try? fm.createDirectory(at: loginPlist.deletingLastPathComponent(), withIntermediateDirectories: true)
        if let d = try? PropertyListSerialization.data(fromPropertyList: plist, format: .xml, options: 0) {
            try? d.write(to: loginPlist)
        }
    }

    // MARK: notifications

    /// notifyWaiting posts once for each new question and takes back the
    /// notifications of the ones answered or gone.
    func notifyWaiting() {
        let waiting = state.waiting ?? []
        var now: [String: String] = [:]
        for w in waiting { now[w.key] = w.sig }
        let gone = notified.keys.filter { now[$0] != notified[$0] }
        if !gone.isEmpty { center.removeDeliveredNotifications(withIdentifiers: gone) }
        for w in waiting where notified[w.key] != w.sig {
            if got && w.seen != true { notify(w) }
        }
        notified = now
    }

    func notify(_ w: Wait) {
        let acts = actions(w)
        var category: String?
        if !acts.isEmpty {
            var buttons = acts.enumerated().map { i, a in
                UNNotificationAction(identifier: "a.\(i)", title: a.0, options: a.0 == "Deny" ? [.destructive] : [])
            }
            if w.kind == "question" {
                buttons.append(UNTextInputNotificationAction(identifier: "text", title: "Answer…", options: [],
                                                             textInputButtonTitle: "Send", textInputPlaceholder: "Your answer"))
            }
            let id = "c." + acts.map { $0.0 }.joined(separator: "\u{1f}") + (w.kind == "question" ? ".text" : "")
            categories[id] = UNNotificationCategory(identifier: id, actions: buttons, intentIdentifiers: [], options: [])
            center.setNotificationCategories(Set(categories.values))
            category = id
        }
        var info: [String: Any] = ["key": w.key, "kind": w.kind ?? ""]
        if let r = w.req { info["req"] = r }
        let subtitle = w.header.map { $0.isEmpty ? "needs you" : $0 } ?? "needs you"
        post(id: w.key, title: w.name, subtitle: subtitle, body: w.text ?? w.needs, category: category, info: info)
    }

    func post(id: String, title: String, subtitle: String?, body: String, category: String?, info: [String: Any]) {
        let c = UNMutableNotificationContent()
        c.title = title
        if let subtitle { c.subtitle = subtitle }
        c.body = body
        c.sound = .default
        c.userInfo = info
        if let category { c.categoryIdentifier = category }
        let req = UNNotificationRequest(identifier: id, content: c, trigger: nil)
        // Categories register asynchronously; reading them back first
        // makes sure the buttons are there when the banner shows.
        center.getNotificationCategories { [center] _ in center.add(req) }
    }

    func userNotificationCenter(_ c: UNUserNotificationCenter, willPresent n: UNNotification,
                                withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
        done([.banner, .list, .sound])
    }

    func userNotificationCenter(_ c: UNUserNotificationCenter, didReceive r: UNNotificationResponse,
                                withCompletionHandler done: @escaping () -> Void) {
        let info = r.notification.request.content.userInfo
        let key = info["key"] as? String ?? ""
        let action = r.actionIdentifier
        DispatchQueue.main.async { [self] in
            defer { done() }
            if action == UNNotificationDefaultActionIdentifier {
                show(key)
                return
            }
            // The buttons were made from the wait as it was; answer it only
            // if it's still asking that.
            guard let w = (state.waiting ?? []).first(where: { $0.key == key }),
                  w.req == info["req"] as? String else { return }
            if action == "text", let t = (r as? UNTextInputNotificationResponse)?.userText,
               !t.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                var op: [String: Any] = ["op": "answer", "key": key, "answer": t]
                if let req = w.req { op["req"] = req }
                send(op)
                return
            }
            let acts = actions(w)
            if action.hasPrefix("a."), let i = Int(action.dropFirst(2)), i < acts.count {
                send(acts[i].1)
            }
        }
    }
}

// MARK: views

let rowWidth: CGFloat = 340
let inset: CGFloat = 14

/// level is the colour for how much of a window is used.
func level(_ pct: Double) -> NSColor {
    pct >= 90 ? .systemRed : pct >= 70 ? .systemOrange : .systemGreen
}

/// MenuView is a menu row drawn by hand, highlighted as the system's own
/// are when it can be clicked.
class MenuView: NSView {
    var action: (() -> Void)?

    var highlighted: Bool { action != nil && (enclosingMenuItem?.isHighlighted ?? false) }

    override func draw(_ r: NSRect) {
        if highlighted {
            NSColor.selectedContentBackgroundColor.setFill()
            NSBezierPath(roundedRect: bounds.insetBy(dx: 5, dy: 0), xRadius: 4, yRadius: 4).fill()
        }
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        trackingAreas.forEach(removeTrackingArea)
        addTrackingArea(NSTrackingArea(rect: bounds, options: [.mouseEnteredAndExited, .activeAlways], owner: self))
    }

    override func mouseEntered(with e: NSEvent) { needsDisplay = true }
    override func mouseExited(with e: NSEvent) { needsDisplay = true }

    override func mouseUp(with e: NSEvent) {
        guard let action else { return }
        enclosingMenuItem?.menu?.cancelTracking()
        action()
    }

    func text(_ s: String, _ font: NSFont, _ color: NSColor, in r: NSRect, right: Bool = false) {
        let p = NSMutableParagraphStyle()
        p.lineBreakMode = .byTruncatingTail
        p.alignment = right ? .right : .left
        let attrs: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: color, .paragraphStyle: p]
        let h = font.ascender - font.descender
        (s as NSString).draw(in: NSRect(x: r.minX, y: r.midY - h / 2, width: r.width, height: h + 1), withAttributes: attrs)
    }

    func width(_ s: String, _ font: NSFont) -> CGFloat {
        s.isEmpty ? 0 : ceil((s as NSString).size(withAttributes: [.font: font]).width)
    }
}

/// RowView is one line: a dot, a name, what it's doing in grey, and a time
/// at the right.
final class RowView: MenuView {
    let title, detail, trailing: String
    let dot: NSColor?

    init(title: String, detail: String = "", trailing: String = "", dot: NSColor? = nil, action: (() -> Void)? = nil) {
        (self.title, self.detail, self.trailing, self.dot) = (title, detail, trailing, dot)
        super.init(frame: NSRect(x: 0, y: 0, width: rowWidth, height: 22))
        self.action = action
        toolTip = [title, detail].filter { !$0.isEmpty }.joined(separator: "\n")
    }

    required init?(coder: NSCoder) { fatalError() }

    override func draw(_ r: NSRect) {
        super.draw(r)
        let hi = highlighted
        let primary: NSColor = hi ? .selectedMenuItemTextColor : .labelColor
        let secondary: NSColor = hi ? .selectedMenuItemTextColor.withAlphaComponent(0.8) : .secondaryLabelColor
        let font = NSFont.menuFont(ofSize: 13), small = NSFont.systemFont(ofSize: 12)
        let digits = NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .regular)
        var x = inset
        if let dot {
            (hi ? .selectedMenuItemTextColor : dot).setFill()
            NSBezierPath(ovalIn: NSRect(x: x, y: bounds.midY - 3.5, width: 7, height: 7)).fill()
            x += 15
        }
        var right = bounds.maxX - inset
        if !trailing.isEmpty {
            let w = width(trailing, digits)
            text(trailing, digits, hi ? secondary : .tertiaryLabelColor, in: NSRect(x: right - w, y: 0, width: w, height: bounds.height))
            right -= w + 10
        }
        guard !title.isEmpty else {
            x = max(x, inset + 15) // under the names, not the dots
            text(detail, small, secondary, in: NSRect(x: x, y: 0, width: right - x, height: bounds.height))
            return
        }
        let room = right - x
        let tw = detail.isEmpty ? room : min(width(title, font), room * 0.5)
        text(title, font, primary, in: NSRect(x: x, y: 0, width: tw, height: bounds.height))
        if !detail.isEmpty {
            let dx = x + tw + 8
            text(detail, small, secondary, in: NSRect(x: dx, y: 0, width: max(0, right - dx), height: bounds.height))
        }
    }
}

/// UsageView is an account's name and its two windows as bars, inline.
final class UsageView: MenuView {
    let a: Acct

    init(_ a: Acct) {
        self.a = a
        super.init(frame: NSRect(x: 0, y: 0, width: rowWidth, height: 24))
    }

    required init?(coder: NSCoder) { fatalError() }

    override func draw(_ r: NSRect) {
        let font = NSFont.menuFont(ofSize: 13)
        let label = NSFont.systemFont(ofSize: 11, weight: .medium)
        let digits = NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .regular)
        let nameW: CGFloat = 96
        text(a.name, font, a.current == true ? .labelColor : .secondaryLabelColor,
             in: NSRect(x: inset, y: 0, width: nameW - 6, height: bounds.height))
        if let p = a.problem, !p.isEmpty, !a.fiveHour.present, !a.sevenDay.present {
            text(p, NSFont.systemFont(ofSize: 11), .tertiaryLabelColor,
                 in: NSRect(x: inset + nameW, y: 0, width: bounds.width - inset * 2 - nameW, height: bounds.height))
            return
        }
        let gap: CGFloat = 14
        let each = (bounds.maxX - inset - (inset + nameW) - gap) / 2
        window("5h", a.fiveHour, x: inset + nameW, w: each, label: label, digits: digits)
        window("wk", a.sevenDay, x: inset + nameW + each + gap, w: each, label: label, digits: digits)
    }

    func window(_ name: String, _ win: Win, x: CGFloat, w: CGFloat, label: NSFont, digits: NSFont) {
        let lw: CGFloat = 20, pw: CGFloat = 32
        text(name, label, .tertiaryLabelColor, in: NSRect(x: x, y: 0, width: lw, height: bounds.height))
        let pct = win.present ? "\(Int(win.percent.rounded()))%" : "–"
        text(pct, digits, win.present ? .secondaryLabelColor : .tertiaryLabelColor,
             in: NSRect(x: x + w - pw, y: 0, width: pw, height: bounds.height), right: true)
        let track = NSRect(x: x + lw, y: bounds.midY - 2.5, width: w - lw - pw - 4, height: 5)
        NSColor.quaternaryLabelColor.setFill()
        NSBezierPath(roundedRect: track, xRadius: 2.5, yRadius: 2.5).fill()
        guard win.present, win.percent > 0 else { return }
        var fill = track
        fill.size.width = max(track.height, track.width * min(1, win.percent / 100))
        level(win.percent).setFill()
        NSBezierPath(roundedRect: fill, xRadius: 2.5, yRadius: 2.5).fill()
    }
}

@main
struct Main {
    static let delegate = AppDelegate()

    static func main() {
        let app = NSApplication.shared
        app.setActivationPolicy(.accessory)
        app.delegate = delegate
        app.run()
    }
}
