function formatDate(d) {
    var prefix = d.toDateString()
    if (prefix === new Date().toDateString()) {
        prefix = "Today"
    }
    return prefix + " " + d.toTimeString().split(" ")[0]
}

function formatYYYYDDMM(d) {
    return d.toJSON().split("T")[0].replace(new RegExp("-", 'g'), "")
}

function dateFromTimestamp(timestamp) {
    return new Date(timestamp * 1000)
}

export {formatDate, formatYYYYDDMM, dateFromTimestamp}