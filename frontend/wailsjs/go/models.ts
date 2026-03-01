export namespace core {
	
	export class ActiveConnection {
	    id: string;
	    network: string;
	    source: string;
	    destination: string;
	    direction: string;
	    // Go type: time
	    lastSeen: any;
	    hits: number;
	
	    static createFrom(source: any = {}) {
	        return new ActiveConnection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.network = source["network"];
	        this.source = source["source"];
	        this.destination = source["destination"];
	        this.direction = source["direction"];
	        this.lastSeen = this.convertValues(source["lastSeen"], null);
	        this.hits = source["hits"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UISettings {
	    language: string;
	    theme: string;
	
	    static createFrom(source: any = {}) {
	        return new UISettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.theme = source["theme"];
	    }
	}
	export class PortForwardRule {
	    id: string;
	    name: string;
	    listen: string;
	    target: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PortForwardRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.listen = source["listen"];
	        this.target = source["target"];
	        this.enabled = source["enabled"];
	    }
	}
	export class ReverseForwarderSettings {
	    dialUrl: string;
	    listenAddr: string;
	    insecure: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ReverseForwarderSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dialUrl = source["dialUrl"];
	        this.listenAddr = source["listenAddr"];
	        this.insecure = source["insecure"];
	    }
	}
	export class ReverseRoute {
	    path: string;
	    target: string;
	    stripPrefix?: boolean;
	    hostHeader: string;
	
	    static createFrom(source: any = {}) {
	        return new ReverseRoute(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.target = source["target"];
	        this.stripPrefix = source["stripPrefix"];
	        this.hostHeader = source["hostHeader"];
	    }
	}
	export class ReverseClientSettings {
	    clientId: string;
	    routes: ReverseRoute[];
	
	    static createFrom(source: any = {}) {
	        return new ReverseClientSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.clientId = source["clientId"];
	        this.routes = this.convertValues(source["routes"], ReverseRoute);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CoreSettings {
	    sudokuBinary: string;
	    hevBinary: string;
	    workingDir: string;
	    localPort: number;
	    allowLan: boolean;
	    logLevel: string;
	    autoStart: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CoreSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sudokuBinary = source["sudokuBinary"];
	        this.hevBinary = source["hevBinary"];
	        this.workingDir = source["workingDir"];
	        this.localPort = source["localPort"];
	        this.allowLan = source["allowLan"];
	        this.logLevel = source["logLevel"];
	        this.autoStart = source["autoStart"];
	    }
	}
	export class TunSettings {
	    enabled: boolean;
	    interfaceName: string;
	    mtu: number;
	    ipv4: string;
	    ipv6: string;
	    socksUdp: string;
	    socksMark: number;
	    routeTable: number;
	    logLevel: string;
	    mapDnsEnabled: boolean;
	    mapDnsAddress: string;
	    mapDnsPort: number;
	    mapDnsNetwork: string;
	    mapDnsNetmask: string;
	    taskStackSize: number;
	    tcpBufferSize: number;
	    maxSession: number;
	    connectTimeout: number;
	
	    static createFrom(source: any = {}) {
	        return new TunSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.interfaceName = source["interfaceName"];
	        this.mtu = source["mtu"];
	        this.ipv4 = source["ipv4"];
	        this.ipv6 = source["ipv6"];
	        this.socksUdp = source["socksUdp"];
	        this.socksMark = source["socksMark"];
	        this.routeTable = source["routeTable"];
	        this.logLevel = source["logLevel"];
	        this.mapDnsEnabled = source["mapDnsEnabled"];
	        this.mapDnsAddress = source["mapDnsAddress"];
	        this.mapDnsPort = source["mapDnsPort"];
	        this.mapDnsNetwork = source["mapDnsNetwork"];
	        this.mapDnsNetmask = source["mapDnsNetmask"];
	        this.taskStackSize = source["taskStackSize"];
	        this.tcpBufferSize = source["tcpBufferSize"];
	        this.maxSession = source["maxSession"];
	        this.connectTimeout = source["connectTimeout"];
	    }
	}
	export class RoutingSettings {
	    proxyMode: string;
	    ruleUrls: string[];
	    customRulesEnabled: boolean;
	    customRules: string;
	
	    static createFrom(source: any = {}) {
	        return new RoutingSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proxyMode = source["proxyMode"];
	        this.ruleUrls = source["ruleUrls"];
	        this.customRulesEnabled = source["customRulesEnabled"];
	        this.customRules = source["customRules"];
	    }
	}
	export class HTTPMaskSettings {
	    disable: boolean;
	    mode: string;
	    tls: boolean;
	    host: string;
	    pathRoot: string;
	    multiplex: string;
	
	    static createFrom(source: any = {}) {
	        return new HTTPMaskSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.disable = source["disable"];
	        this.mode = source["mode"];
	        this.tls = source["tls"];
	        this.host = source["host"];
	        this.pathRoot = source["pathRoot"];
	        this.multiplex = source["multiplex"];
	    }
	}
	export class NodeConfig {
	    id: string;
	    name: string;
	    serverAddress: string;
	    key: string;
	    aead: string;
	    ascii: string;
	    paddingMin: number;
	    paddingMax: number;
	    enablePureDownlink: boolean;
	    customTable: string;
	    customTables: string[];
	    httpMask: HTTPMaskSettings;
	    localPort: number;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NodeConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.serverAddress = source["serverAddress"];
	        this.key = source["key"];
	        this.aead = source["aead"];
	        this.ascii = source["ascii"];
	        this.paddingMin = source["paddingMin"];
	        this.paddingMax = source["paddingMax"];
	        this.enablePureDownlink = source["enablePureDownlink"];
	        this.customTable = source["customTable"];
	        this.customTables = source["customTables"];
	        this.httpMask = this.convertValues(source["httpMask"], HTTPMaskSettings);
	        this.localPort = source["localPort"];
	        this.enabled = source["enabled"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AppConfig {
	    version: number;
	    activeNodeId: string;
	    nodes: NodeConfig[];
	    routing: RoutingSettings;
	    tun: TunSettings;
	    core: CoreSettings;
	    reverseClient: ReverseClientSettings;
	    reverseForward: ReverseForwarderSettings;
	    portForwards: PortForwardRule[];
	    ui: UISettings;
	    lastStartedNode: string;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.activeNodeId = source["activeNodeId"];
	        this.nodes = this.convertValues(source["nodes"], NodeConfig);
	        this.routing = this.convertValues(source["routing"], RoutingSettings);
	        this.tun = this.convertValues(source["tun"], TunSettings);
	        this.core = this.convertValues(source["core"], CoreSettings);
	        this.reverseClient = this.convertValues(source["reverseClient"], ReverseClientSettings);
	        this.reverseForward = this.convertValues(source["reverseForward"], ReverseForwarderSettings);
	        this.portForwards = this.convertValues(source["portForwards"], PortForwardRule);
	        this.ui = this.convertValues(source["ui"], UISettings);
	        this.lastStartedNode = source["lastStartedNode"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BandwidthSample {
	    // Go type: time
	    at: any;
	    txBps: number;
	    rxBps: number;
	    directBps: number;
	    proxyBps: number;
	    totalTx: number;
	    totalRx: number;
	
	    static createFrom(source: any = {}) {
	        return new BandwidthSample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.at = this.convertValues(source["at"], null);
	        this.txBps = source["txBps"];
	        this.rxBps = source["rxBps"];
	        this.directBps = source["directBps"];
	        this.proxyBps = source["proxyBps"];
	        this.totalTx = source["totalTx"];
	        this.totalRx = source["totalRx"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class IPDetectResult {
	    ip: string;
	    region: string;
	    country: string;
	    isp: string;
	    source: string;
	    usedProxy: boolean;
	    checkedAtUnix: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new IPDetectResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ip = source["ip"];
	        this.region = source["region"];
	        this.country = source["country"];
	        this.isp = source["isp"];
	        this.source = source["source"];
	        this.usedProxy = source["usedProxy"];
	        this.checkedAtUnix = source["checkedAtUnix"];
	        this.error = source["error"];
	    }
	}
	export class LatencyResult {
	    nodeId: string;
	    nodeName: string;
	    latencyMs: number;
	    connectOk: boolean;
	    statusCode: number;
	    checkedAtUnix: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new LatencyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.nodeName = source["nodeName"];
	        this.latencyMs = source["latencyMs"];
	        this.connectOk = source["connectOk"];
	        this.statusCode = source["statusCode"];
	        this.checkedAtUnix = source["checkedAtUnix"];
	        this.error = source["error"];
	    }
	}
	export class LogEntry {
	    id: string;
	    // Go type: time
	    timestamp: any;
	    level: string;
	    component: string;
	    message: string;
	    raw: string;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.level = source["level"];
	        this.component = source["component"];
	        this.message = source["message"];
	        this.raw = source["raw"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	export class TrafficState {
	    totalTx: number;
	    totalRx: number;
	    estimatedDirectTx: number;
	    estimatedDirectRx: number;
	    estimatedProxyTx: number;
	    estimatedProxyRx: number;
	    directConnDecisions: number;
	    proxyConnDecisions: number;
	    recentBandwidth: BandwidthSample[];
	    interface: string;
	    interfaceFound: boolean;
	    lastSampleUnixMillis: number;
	
	    static createFrom(source: any = {}) {
	        return new TrafficState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalTx = source["totalTx"];
	        this.totalRx = source["totalRx"];
	        this.estimatedDirectTx = source["estimatedDirectTx"];
	        this.estimatedDirectRx = source["estimatedDirectRx"];
	        this.estimatedProxyTx = source["estimatedProxyTx"];
	        this.estimatedProxyRx = source["estimatedProxyRx"];
	        this.directConnDecisions = source["directConnDecisions"];
	        this.proxyConnDecisions = source["proxyConnDecisions"];
	        this.recentBandwidth = this.convertValues(source["recentBandwidth"], BandwidthSample);
	        this.interface = source["interface"];
	        this.interfaceFound = source["interfaceFound"];
	        this.lastSampleUnixMillis = source["lastSampleUnixMillis"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RuntimeState {
	    running: boolean;
	    coreRunning: boolean;
	    tunRunning: boolean;
	    reverseRunning: boolean;
	    startedAtUnix: number;
	    activeNodeId: string;
	    activeNodeName: string;
	    lastError: string;
	    traffic: TrafficState;
	    latencies: LatencyResult[];
	    connections: ActiveConnection[];
	    recentLogs: LogEntry[];
	    needsAdmin: boolean;
	    routeSetupError: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.coreRunning = source["coreRunning"];
	        this.tunRunning = source["tunRunning"];
	        this.reverseRunning = source["reverseRunning"];
	        this.startedAtUnix = source["startedAtUnix"];
	        this.activeNodeId = source["activeNodeId"];
	        this.activeNodeName = source["activeNodeName"];
	        this.lastError = source["lastError"];
	        this.traffic = this.convertValues(source["traffic"], TrafficState);
	        this.latencies = this.convertValues(source["latencies"], LatencyResult);
	        this.connections = this.convertValues(source["connections"], ActiveConnection);
	        this.recentLogs = this.convertValues(source["recentLogs"], LogEntry);
	        this.needsAdmin = source["needsAdmin"];
	        this.routeSetupError = source["routeSetupError"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class StartRequest {
	    withTun: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StartRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.withTun = source["withTun"];
	    }
	}
	
	
	
	export class UsageDay {
	    date: string;
	    tx: number;
	    rx: number;
	    directTx: number;
	    directRx: number;
	    proxyTx: number;
	    proxyRx: number;
	
	    static createFrom(source: any = {}) {
	        return new UsageDay(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.tx = source["tx"];
	        this.rx = source["rx"];
	        this.directTx = source["directTx"];
	        this.directRx = source["directRx"];
	        this.proxyTx = source["proxyTx"];
	        this.proxyRx = source["proxyRx"];
	    }
	}

}

